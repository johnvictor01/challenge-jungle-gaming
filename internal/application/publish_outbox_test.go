package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type dispatchRepository struct {
	events    []OutboxEvent
	claimed   []OutboxEvent
	published []string
	retries   map[string]time.Time
	claimErr  error
}

func (r *dispatchRepository) Claim(context.Context, string, int, time.Duration) ([]OutboxEvent, error) {
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	r.claimed = append([]OutboxEvent(nil), r.events...)
	return r.claimed, nil
}
func (r *dispatchRepository) MarkPublished(_ context.Context, id, _ string, _ time.Time) error {
	r.published = append(r.published, id)
	return nil
}
func (r *dispatchRepository) ScheduleRetry(_ context.Context, id, _ string, next time.Time, _ string) error {
	if r.retries == nil {
		r.retries = map[string]time.Time{}
	}
	r.retries[id] = next
	return nil
}

type dispatchPublisher struct {
	err    error
	events []OutboxEvent
}

func (p *dispatchPublisher) Publish(_ context.Context, event OutboxEvent) error {
	p.events = append(p.events, event)
	return p.err
}

func TestOutboxDispatcherPublishesAndConfirmsCommittedEvent(t *testing.T) {
	repo := &dispatchRepository{events: []OutboxEvent{{EventID: "event-1"}}}
	publisher := &dispatchPublisher{}
	dispatcher, err := NewOutboxDispatcher(repo, publisher, OutboxDispatchConfig{Owner: "worker-a"})
	if err != nil {
		t.Fatal(err)
	}
	count, err := dispatcher.DispatchBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(publisher.events) != 1 || len(repo.published) != 1 || repo.published[0] != "event-1" {
		t.Fatalf("dispatch did not publish and confirm the event: count=%d published=%v", count, repo.published)
	}
}

func TestOutboxDispatcherSchedulesExponentialRetryAfterPublishFailure(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &dispatchRepository{events: []OutboxEvent{{EventID: "event-2", Attempts: 2}}}
	publisher := &dispatchPublisher{err: errors.New("SQS unavailable")}
	dispatcher, err := NewOutboxDispatcher(repo, publisher, OutboxDispatchConfig{
		Owner: "worker-a", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.DispatchBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := repo.retries["event-2"], now.Add(4*time.Second); !got.Equal(want) {
		t.Fatalf("retry time = %s, want %s", got, want)
	}
	if len(repo.published) != 0 {
		t.Fatal("failed event must not be marked published")
	}
}

func TestExponentialOutboxRetryDelayCapsAtOneMinute(t *testing.T) {
	if got := ExponentialOutboxRetryDelay(99); got != time.Minute {
		t.Fatalf("retry delay = %s, want one minute", got)
	}
}
