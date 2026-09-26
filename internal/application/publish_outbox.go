package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/observability"
)

var ErrOutboxLeaseLost = errors.New("outbox lease is no longer owned")

// OutboxPublisher sends one committed event. Implementations must retain EventID
// in the envelope so downstream consumers can make repeated delivery harmless.
type OutboxPublisher interface {
	Publish(ctx context.Context, event OutboxEvent) error
}

type OutboxDispatchConfig struct {
	Owner       string
	Logger      *slog.Logger
	BatchSize   int
	Lease       time.Duration
	RetryDelay  func(attempt int) time.Duration
	Now         func() time.Time
	MaxAttempts int
}

// OutboxDispatcher claims committed events, publishes them and records either
// delivery or a durable retry schedule. Delivery is at-least-once by design.
type OutboxDispatcher struct {
	repository OutboxDispatchRepository
	publisher  OutboxPublisher
	config     OutboxDispatchConfig
	logger     *slog.Logger
}

func NewOutboxDispatcher(repository OutboxDispatchRepository, publisher OutboxPublisher, config OutboxDispatchConfig) (*OutboxDispatcher, error) {
	config.Owner = strings.TrimSpace(config.Owner)
	if config.Owner == "" || repository == nil || publisher == nil {
		return nil, errors.New("outbox owner, repository and publisher are required")
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}
	if config.Lease <= 0 {
		config.Lease = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.RetryDelay == nil {
		config.RetryDelay = ExponentialOutboxRetryDelay
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 10
	}
	return &OutboxDispatcher{repository: repository, publisher: publisher, config: config, logger: config.Logger}, nil
}

func ExponentialOutboxRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second
	for i := 1; i < attempt && delay < time.Minute; i++ {
		delay *= 2
	}
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}

// DispatchBatch returns the number of events claimed. Per-event publish errors
// are persisted as retries and do not prevent the rest of the batch from running.
func (d *OutboxDispatcher) DispatchBatch(ctx context.Context) (int, error) {
	events, err := d.repository.Claim(ctx, d.config.Owner, d.config.BatchSize, d.config.Lease)
	if err != nil {
		return 0, fmt.Errorf("claim outbox events: %w", err)
	}
	for _, event := range events {
		lag := d.config.Now().Sub(event.OccurredAt)
		if lag < 0 {
			lag = 0
		}
		observability.Default.Observe("outbox_event_lag", lag)
		if err := d.publisher.Publish(ctx, event); err != nil {
			observability.Default.Inc("outbox_publish_retry")
			d.logger.Error("outbox event publish failed", "event_id", event.EventID, "event_type", event.EventType, "aggregate_id", event.AggregateID, "correlation_id", event.CorrelationID, "attempt", event.Attempts, "error", err)
			if event.Attempts >= d.config.MaxAttempts {
				if scheduleErr := d.repository.MarkFailed(ctx, event.EventID, d.config.Owner, err.Error()); scheduleErr != nil {
					return len(events), errors.Join(fmt.Errorf("publish outbox event %s: %w", event.EventID, err), fmt.Errorf("mark failed: %w", scheduleErr))
				}
				d.logger.Error("outbox event moved to terminal failure", "event_id", event.EventID, "attempt", event.Attempts)
				observability.Default.Inc("outbox_publish_failed")
				continue
			}
			next := d.config.Now().Add(d.config.RetryDelay(event.Attempts))
			if scheduleErr := d.repository.ScheduleRetry(ctx, event.EventID, d.config.Owner, next, err.Error()); scheduleErr != nil {
				return len(events), errors.Join(fmt.Errorf("publish outbox event %s: %w", event.EventID, err), fmt.Errorf("schedule retry: %w", scheduleErr))
			}
			continue
		}
		if err := d.repository.MarkPublished(ctx, event.EventID, d.config.Owner, d.config.Now()); err != nil {
			return len(events), fmt.Errorf("mark outbox event %s published: %w", event.EventID, err)
		}
		observability.Default.Inc("outbox_event_published")
		d.logger.Info("outbox event published", "event_id", event.EventID, "event_type", event.EventType, "aggregate_id", event.AggregateID, "correlation_id", event.CorrelationID)
	}
	return len(events), nil
}
