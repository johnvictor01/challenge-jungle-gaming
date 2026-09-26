package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrOutboxLeaseLost = errors.New("outbox lease is no longer owned")

// OutboxPublisher sends one committed event. Implementations must retain EventID
// in the envelope so downstream consumers can make repeated delivery harmless.
type OutboxPublisher interface {
	Publish(ctx context.Context, event OutboxEvent) error
}

type OutboxDispatchConfig struct {
	Owner      string
	BatchSize  int
	Lease      time.Duration
	RetryDelay func(attempt int) time.Duration
	Now        func() time.Time
}

// OutboxDispatcher claims committed events, publishes them and records either
// delivery or a durable retry schedule. Delivery is at-least-once by design.
type OutboxDispatcher struct {
	repository OutboxDispatchRepository
	publisher  OutboxPublisher
	config     OutboxDispatchConfig
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
	if config.RetryDelay == nil {
		config.RetryDelay = ExponentialOutboxRetryDelay
	}
	return &OutboxDispatcher{repository: repository, publisher: publisher, config: config}, nil
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
		if err := d.publisher.Publish(ctx, event); err != nil {
			next := d.config.Now().Add(d.config.RetryDelay(event.Attempts + 1))
			if scheduleErr := d.repository.ScheduleRetry(ctx, event.EventID, d.config.Owner, next, err.Error()); scheduleErr != nil {
				return len(events), errors.Join(fmt.Errorf("publish outbox event %s: %w", event.EventID, err), fmt.Errorf("schedule retry: %w", scheduleErr))
			}
			continue
		}
		if err := d.repository.MarkPublished(ctx, event.EventID, d.config.Owner, d.config.Now()); err != nil {
			return len(events), fmt.Errorf("mark outbox event %s published: %w", event.EventID, err)
		}
	}
	return len(events), nil
}
