package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

// ReferenceRetryPolicy define por quanto tempo uma operação pode esperar por sua
// referência. A política fica no application porque é comportamento do worker,
// não uma regra pura da entidade.
type ReferenceRetryPolicy struct {
	MaxAttempts int
	RetryDelay  func(attempt int) time.Duration
}

func defaultReferenceRetryPolicy() ReferenceRetryPolicy {
	return ReferenceRetryPolicy{
		MaxAttempts: 10,
		RetryDelay: func(attempt int) time.Duration {
			if attempt > 6 {
				attempt = 6
			}
			return time.Minute * time.Duration(1<<uint(attempt-1))
		},
	}
}

// ResolvePendingReferenceService é chamado pelo worker para retomar uma
// REFUND, ROLLBACK ou WIN que chegou antes da operação referenciada.
type ResolvePendingReferenceService struct {
	uow    UnitOfWork
	ids    IDGenerator
	policy ReferenceRetryPolicy
	now    func() time.Time
}

func NewResolvePendingReferenceService(uow UnitOfWork, ids IDGenerator, policy ReferenceRetryPolicy) *ResolvePendingReferenceService {
	defaultPolicy := defaultReferenceRetryPolicy()
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = defaultPolicy.MaxAttempts
	}
	if policy.RetryDelay == nil {
		policy.RetryDelay = defaultPolicy.RetryDelay
	}
	return &ResolvePendingReferenceService{uow: uow, ids: ids, policy: policy, now: time.Now}
}

// Execute processa uma operação pendente pelo seu ID. Quando a referência ainda
// não existe, grava a próxima tentativa; quando o limite termina, a operação é
// rejeitada de forma auditável.
func (s *ResolvePendingReferenceService) Execute(ctx context.Context, transactionID string) (ProcessWagerResult, error) {
	if s == nil || s.uow == nil || s.ids == nil || transactionID == "" {
		return ProcessWagerResult{}, errors.New("reference resolver is not configured")
	}
	var result ProcessWagerResult
	err := s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		transaction, err := repositories.Transactions.FindByIDForUpdate(ctx, transactionID)
		if err != nil {
			return err
		}
		if transaction.Status != domain.TransactionPendingReference {
			result = resultFromTransaction(transaction, transaction.Status == domain.TransactionProcessed || transaction.Status == domain.TransactionRejected || transaction.Status == domain.TransactionFailed)
			return nil
		}
		return s.resolve(ctx, repositories, transaction, &result)
	})
	if err != nil {
		return ProcessWagerResult{}, err
	}
	return result, nil
}

func (s *ResolvePendingReferenceService) resolve(ctx context.Context, repositories Repositories, transaction *domain.WagerTransaction, result *ProcessWagerResult) error {
	correlationID := transaction.ID
	reference, err := repositories.Transactions.FindByExternalID(ctx, transaction.ProviderID, transaction.ReferenceExternalTransactionID)
	if errors.Is(err, ErrNotFound) || (err == nil && (reference == nil || reference.Status == domain.TransactionPending || reference.Status == domain.TransactionPendingReference)) {
		return s.retryOrReject(ctx, repositories, transaction, correlationID, result)
	}
	if err != nil {
		return err
	}
	if reference.Status == domain.TransactionRejected || reference.Status == domain.TransactionFailed {
		return s.reject(ctx, repositories, transaction, "REFERENCE_NOT_PROCESSED", correlationID, result)
	}
	if err := transaction.ResumeAfterReference(*reference); err != nil {
		if errors.Is(err, domain.ErrInvalidReference) {
			return s.reject(ctx, repositories, transaction, "INVALID_REFERENCE", correlationID, result)
		}
		return err
	}
	if transaction.Kind == domain.TransactionRefund || transaction.Kind == domain.TransactionRollback {
		reversal, err := repositories.Transactions.FindSuccessfulReversal(ctx, reference.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if reversal != nil {
			return s.reject(ctx, repositories, transaction, "REFERENCE_ALREADY_REVERSED", correlationID, result)
		}
	}

	wallet, err := repositories.Wallets.FindByID(ctx, transaction.WalletID)
	if err != nil {
		return err
	}
	before, version := wallet.Balance, wallet.Version
	if err := applyWalletOperation(wallet, transaction, reference); err != nil {
		if errors.Is(err, domain.ErrInsufficientFunds) {
			code := "INSUFFICIENT_FUNDS"
			if transaction.Kind == domain.TransactionRollback {
				code = "REVERSAL_INSUFFICIENT_FUNDS"
			}
			return s.reject(ctx, repositories, transaction, code, correlationID, result)
		}
		return err
	}
	if wallet.Version != version {
		if err := repositories.Wallets.Update(ctx, wallet, version); err != nil {
			return err
		}
	}
	if err := transaction.MarkProcessed(wallet.Balance); err != nil {
		return err
	}
	if err := repositories.Transactions.Update(ctx, transaction); err != nil {
		return err
	}
	writer := &ProcessWagerService{ids: s.ids}
	if wallet.Version != version {
		ledgerID, err := s.ids.NewID()
		if err != nil {
			return err
		}
		direction := domain.DirectionCredit
		if wallet.Balance.Units < before.Units {
			direction = domain.DirectionDebit
		}
		entry, err := domain.NewWalletLedgerEntry(ledgerID, wallet.ID, transaction.ID, direction, transaction.Amount, before, wallet.Balance)
		if err != nil {
			return err
		}
		if err := repositories.Ledger.Create(ctx, entry); err != nil {
			return err
		}
	}
	if err := writer.appendProcessedEvent(ctx, repositories, transaction, correlationID); err != nil {
		return err
	}
	if wallet.Version != version {
		if err := writer.appendBalanceChangedEvent(ctx, repositories, transaction, before, wallet, correlationID); err != nil {
			return err
		}
	}
	*result = resultFromTransaction(transaction, false)
	return nil
}

func (s *ResolvePendingReferenceService) retryOrReject(ctx context.Context, repositories Repositories, transaction *domain.WagerTransaction, correlationID string, result *ProcessWagerResult) error {
	nextAttempt := transaction.AttemptCount + 1
	if nextAttempt >= s.policy.MaxAttempts {
		transaction.AttemptCount = nextAttempt
		return s.reject(ctx, repositories, transaction, "REFERENCE_NOT_FOUND", correlationID, result)
	}
	now := s.now().UTC()
	if err := transaction.ScheduleReferenceRetry(now.Add(s.policy.RetryDelay(nextAttempt))); err != nil {
		return err
	}
	if err := repositories.Transactions.Update(ctx, transaction); err != nil {
		return err
	}
	*result = resultFromTransaction(transaction, false)
	return nil
}

func (s *ResolvePendingReferenceService) reject(ctx context.Context, repositories Repositories, transaction *domain.WagerTransaction, code, correlationID string, result *ProcessWagerResult) error {
	if err := transaction.Reject(code); err != nil {
		return fmt.Errorf("reject pending reference: %w", err)
	}
	if err := repositories.Transactions.Update(ctx, transaction); err != nil {
		return err
	}
	writer := &ProcessWagerService{ids: s.ids}
	if err := writer.appendRejectedEvent(ctx, repositories, transaction, correlationID); err != nil {
		return err
	}
	*result = resultFromTransaction(transaction, false)
	return nil
}
