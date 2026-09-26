package postgres

import (
	"context"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type ledgerRepository struct{ tx pgx.Tx }

func (r ledgerRepository) Create(ctx context.Context, entry *domain.WalletLedgerEntry) error {
	_, err := r.tx.Exec(ctx, `INSERT INTO wallet_ledger_entries
        (id, wallet_id, transaction_id, direction, amount_minor, currency,
         balance_before_minor, balance_after_minor, created_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		entry.ID, entry.WalletID, entry.TransactionID, string(entry.Direction), entry.Amount.Units,
		entry.Amount.Currency, entry.BalanceBefore.Units, entry.BalanceAfter.Units, entry.CreatedAt)
	return mapError(err)
}

func (r ledgerRepository) ListByWallet(ctx context.Context, walletID string, afterCreatedAt *time.Time, afterID string, limit int) ([]*domain.WalletLedgerEntry, error) {
	var cursorID any
	if afterCreatedAt != nil {
		cursorID = afterID
	}
	rows, err := r.tx.Query(ctx, `SELECT id, wallet_id, transaction_id, direction, amount_minor, currency,
        balance_before_minor, balance_after_minor, created_at
        FROM wallet_ledger_entries
        WHERE wallet_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) > ($2, $3))
		ORDER BY created_at, id LIMIT $4`, walletID, afterCreatedAt, cursorID, limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	entries := make([]*domain.WalletLedgerEntry, 0)
	for rows.Next() {
		var id, walletID, transactionID, direction, currency string
		var amount, before, after int64
		var createdAt time.Time
		if err := rows.Scan(&id, &walletID, &transactionID, &direction, &amount, &currency, &before, &after, &createdAt); err != nil {
			return nil, mapError(err)
		}
		entry, err := domain.RehydrateWalletLedgerEntry(id, walletID, transactionID, domain.Direction(direction),
			domain.Money{Units: amount, Currency: currency}, domain.Money{Units: before, Currency: currency}, domain.Money{Units: after, Currency: currency}, createdAt)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return entries, nil
}

func (r ledgerRepository) SummarizeByWallet(ctx context.Context, walletID string) (application.LedgerSummary, error) {
	var creditsText, debitsText string
	var count int64
	if err := r.tx.QueryRow(ctx, `SELECT
        COALESCE(SUM(amount_minor) FILTER (WHERE direction = 'CREDIT'), 0)::text,
        COALESCE(SUM(amount_minor) FILTER (WHERE direction = 'DEBIT'), 0)::text,
        COUNT(*)
        FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID).Scan(&creditsText, &debitsText, &count); err != nil {
		return application.LedgerSummary{}, mapError(err)
	}
	credits, ok := new(big.Int).SetString(creditsText, 10)
	if !ok || !credits.IsInt64() {
		return application.LedgerSummary{}, domain.ErrOverflow
	}
	debits, ok := new(big.Int).SetString(debitsText, 10)
	if !ok || !debits.IsInt64() {
		return application.LedgerSummary{}, domain.ErrOverflow
	}
	return application.LedgerSummary{Credits: credits.Int64(), Debits: debits.Int64(), Count: count}, nil
}

type outboxRepository struct{ tx pgx.Tx }

func (r outboxRepository) Append(ctx context.Context, event application.OutboxEvent) error {
	_, err := r.tx.Exec(ctx, `INSERT INTO outbox_events
        (event_id, aggregate_id, event_type, event_version, correlation_id,
         causation_id, payload, occurred_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		event.EventID, event.AggregateID, event.EventType, event.Version, event.CorrelationID,
		nullableString(event.CausationID), event.Data, event.OccurredAt)
	return mapError(err)
}

var _ application.WalletLedgerRepository = ledgerRepository{}
var _ application.OutboxRepository = outboxRepository{}
