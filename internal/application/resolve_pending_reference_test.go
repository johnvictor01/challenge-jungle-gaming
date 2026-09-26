package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type failOnceReferenceUnitOfWork struct {
	UnitOfWork
	remaining int
}

func (u *failOnceReferenceUnitOfWork) WithinTransaction(ctx context.Context, callback func(Repositories) error) error {
	if u.remaining > 0 {
		u.remaining--
		return errors.New("temporary database outage")
	}
	return u.UnitOfWork.WithinTransaction(ctx, callback)
}

func pendingRefund(t *testing.T, state *memoryState) (*domain.Wallet, *memoryUnitOfWork, string) {
	t.Helper()
	wallet := seedMemoryWallet(t, state, 500)
	processor, uow := processService(state)
	command := wagerCommand(wallet, domain.TransactionRefund, 1_500, "refund-key", "refund-1")
	command.ReferenceExternalTransactionID = "bet-1"
	result, err := processor.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("create pending refund: %v", err)
	}
	if result.Status != domain.TransactionPendingReference {
		t.Fatalf("status = %s, want PENDING_REFERENCE", result.Status)
	}
	return wallet, uow, result.TransactionID
}

func TestResolvePendingReferenceProcessesWhenReferenceArrives(t *testing.T) {
	state := newMemoryState()
	wallet, uow, pendingID := pendingRefund(t, state)
	uow.state.transactions["bet-id"] = processedExternalTransaction(wallet, "bet-id", "bet-1", domain.TransactionBet, 1_500, "")
	resolver := NewResolvePendingReferenceService(uow, &sequenceIDGenerator{}, ReferenceRetryPolicy{})
	result, err := resolver.Execute(context.Background(), pendingID)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionProcessed || result.Balance == nil || result.Balance.Units() != 2_000 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(uow.state.ledger) != 1 || uow.state.wallets[wallet.ID()].Balance().Units() != 2_000 {
		t.Errorf("resolution did not persist balance and ledger")
	}
	if len(uow.state.events) != 3 {
		t.Errorf("events = %d, want initial pending plus two processing events", len(uow.state.events))
	}
}

func TestResolvePendingReferenceSchedulesRetry(t *testing.T) {
	state := newMemoryState()
	_, uow, pendingID := pendingRefund(t, state)
	resolver := NewResolvePendingReferenceService(uow, &sequenceIDGenerator{}, ReferenceRetryPolicy{MaxAttempts: 3, RetryDelay: func(int) time.Duration { return time.Minute }})
	fixedNow := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return fixedNow }
	result, err := resolver.Execute(context.Background(), pendingID)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionPendingReference {
		t.Fatalf("status = %s, want PENDING_REFERENCE", result.Status)
	}
	pending := uow.state.transactions[pendingID]
	if pending.AttemptCount() != 1 || pending.NextAttemptAt() == nil || !pending.NextAttemptAt().Equal(fixedNow.Add(time.Minute)) {
		t.Errorf("retry state = %+v", pending)
	}
}

func TestResolvePendingReferenceRejectsAfterRetryLimit(t *testing.T) {
	state := newMemoryState()
	_, uow, pendingID := pendingRefund(t, state)
	resolver := NewResolvePendingReferenceService(uow, &sequenceIDGenerator{}, ReferenceRetryPolicy{MaxAttempts: 1, RetryDelay: func(int) time.Duration { return time.Minute }})
	result, err := resolver.Execute(context.Background(), pendingID)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionRejected || result.FailureCode != "REFERENCE_NOT_FOUND" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := uow.state.transactions[pendingID].AttemptCount(); got != 1 {
		t.Errorf("attempt count = %d, want 1", got)
	}
	if len(uow.state.ledger) != 0 || len(uow.state.events) != 2 {
		t.Errorf("expired reference should add only its rejection event")
	}
}

func TestResolvePendingReferenceMarksFailedAfterRepeatedInfrastructureErrors(t *testing.T) {
	state := newMemoryState()
	_, uow, pendingID := pendingRefund(t, state)
	failingUOW := &failOnceReferenceUnitOfWork{UnitOfWork: uow, remaining: 1}
	resolver := NewResolvePendingReferenceService(failingUOW, &sequenceIDGenerator{}, ReferenceRetryPolicy{MaxAttempts: 1, RetryDelay: func(int) time.Duration { return time.Minute }})
	result, err := resolver.Execute(context.Background(), pendingID)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionFailed || result.FailureCode != "REFERENCE_RESOLUTION_FAILED" {
		t.Fatalf("result = %+v, want terminal infrastructure failure", result)
	}
	if len(uow.state.events) != 2 || uow.state.events[1].EventType != "WagerTransactionFailed" {
		t.Fatalf("events = %+v, want persisted failure event", uow.state.events)
	}
	if uow.state.transactions[pendingID].AttemptCount() != 1 {
		t.Fatalf("attempt count = %d, want 1", uow.state.transactions[pendingID].AttemptCount())
	}
}

func TestResolvePendingReferenceSchedulesRetryAfterInfrastructureError(t *testing.T) {
	state := newMemoryState()
	_, uow, pendingID := pendingRefund(t, state)
	failingUOW := &failOnceReferenceUnitOfWork{UnitOfWork: uow, remaining: 1}
	resolver := NewResolvePendingReferenceService(failingUOW, &sequenceIDGenerator{}, ReferenceRetryPolicy{MaxAttempts: 2, RetryDelay: func(int) time.Duration { return time.Minute }})
	fixedNow := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return fixedNow }
	result, err := resolver.Execute(context.Background(), pendingID)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionPendingReference {
		t.Fatalf("status = %s, want PENDING_REFERENCE", result.Status)
	}
	pending := uow.state.transactions[pendingID]
	if pending.AttemptCount() != 1 || pending.NextAttemptAt() == nil || !pending.NextAttemptAt().Equal(fixedNow.Add(time.Minute)) {
		t.Fatalf("retry state = %+v", pending.State())
	}
	if len(uow.state.events) != 2 || uow.state.events[1].EventType != "WagerTransactionRetryScheduled" {
		t.Fatalf("events = %+v, want retry event", uow.state.events)
	}
}
