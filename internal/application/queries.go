package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/observability"
)

var ErrInvalidCursor = errors.New("invalid ledger cursor")

type LedgerPage struct {
	Entries    []*domain.WalletLedgerEntry
	NextCursor string
}

type Reconciliation struct {
	WalletID          string
	StoredBalance     domain.Money
	CalculatedBalance domain.Money
	Difference        domain.Money
	Consistent        bool
	CheckedEntries    int64
}

type QueryService struct{ uow UnitOfWork }

func NewQueryService(uow UnitOfWork) *QueryService { return &QueryService{uow: uow} }

func (s *QueryService) Wallet(ctx context.Context, walletID string) (*domain.Wallet, error) {
	var result *domain.Wallet
	err := s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		wallet, err := repositories.Wallets.FindByID(ctx, walletID)
		if err != nil {
			return err
		}
		result = wallet
		return nil
	})
	return result, err
}

func (s *QueryService) Transaction(ctx context.Context, transactionID string) (*domain.WagerTransaction, error) {
	var result *domain.WagerTransaction
	err := s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		transaction, err := repositories.Transactions.FindByID(ctx, transactionID)
		if err != nil {
			return err
		}
		result = transaction
		return nil
	})
	return result, err
}

func (s *QueryService) ProviderTransaction(ctx context.Context, providerID, externalID string) (*domain.WagerTransaction, error) {
	var result *domain.WagerTransaction
	err := s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		transaction, err := repositories.Transactions.FindByExternalID(ctx, providerID, externalID)
		if err != nil {
			return err
		}
		result = transaction
		return nil
	})
	return result, err
}

func (s *QueryService) Ledger(ctx context.Context, walletID, cursor string, limit int) (LedgerPage, error) {
	if limit <= 0 || limit > 100 {
		return LedgerPage{}, fmt.Errorf("limit must be between 1 and 100")
	}
	var afterAt *time.Time
	var afterID string
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return LedgerPage{}, ErrInvalidCursor
		}
		parts := strings.SplitN(string(decoded), "|", 2)
		if len(parts) != 2 || parts[1] == "" {
			return LedgerPage{}, ErrInvalidCursor
		}
		parsed, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return LedgerPage{}, ErrInvalidCursor
		}
		afterAt, afterID = &parsed, parts[1]
	}
	var entries []*domain.WalletLedgerEntry
	err := s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		if _, err := repositories.Wallets.FindByID(ctx, walletID); err != nil {
			return err
		}
		var err error
		entries, err = repositories.Ledger.ListByWallet(ctx, walletID, afterAt, afterID, limit+1)
		return err
	})
	if err != nil {
		return LedgerPage{}, err
	}
	page := LedgerPage{}
	if len(entries) > limit {
		entries = entries[:limit]
		last := entries[len(entries)-1]
		cursorValue := last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(cursorValue))
	}
	page.Entries = entries
	return page, nil
}

func (s *QueryService) Reconcile(ctx context.Context, walletID string) (Reconciliation, error) {
	started := time.Now()
	defer func() { observability.Default.Observe("reconciliation_latency", time.Since(started)) }()
	var result Reconciliation
	err := s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		wallet, err := repositories.Wallets.FindByID(ctx, walletID)
		if err != nil {
			return err
		}
		summary, err := repositories.Ledger.SummarizeByWallet(ctx, walletID)
		if err != nil {
			return err
		}
		if summary.Debits > summary.Credits {
			return domain.ErrNegativeBalance
		}
		calculated := domain.Money{Units: summary.Credits - summary.Debits, Currency: wallet.Currency}
		difference, err := wallet.Balance.Subtract(calculated)
		if err != nil {
			return err
		}
		result = Reconciliation{WalletID: wallet.ID, StoredBalance: wallet.Balance, CalculatedBalance: calculated, Difference: difference, Consistent: difference.Units == 0, CheckedEntries: summary.Count}
		return nil
	})
	if err == nil && !result.Consistent {
		observability.Default.Inc("reconciliation_divergence")
	}
	return result, err
}
