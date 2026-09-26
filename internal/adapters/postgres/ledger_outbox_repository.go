package postgres

import (
	"context"
	"encoding/json"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// OutboxDispatcherRepository usa o pool fora da UnitOfWork do domínio: cada claim
// e confirmação é uma transação curta, separada do envio de rede ao SQS.
type OutboxDispatcherRepository struct{ pool *pgxpool.Pool }

func NewOutboxDispatcherRepository(store *Store) *OutboxDispatcherRepository {
	return &OutboxDispatcherRepository{pool: store.pool}
}

func (r *OutboxDispatcherRepository) Claim(ctx context.Context, owner string, limit int, lease time.Duration) ([]application.OutboxEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `WITH candidates AS (
		SELECT e.event_id FROM outbox_events AS e
		WHERE e.published_at IS NULL AND e.next_attempt_at <= now()
		  AND (e.lease_until IS NULL OR e.lease_until <= now())
		  AND NOT EXISTS (
			SELECT 1 FROM outbox_events AS earlier
			WHERE earlier.aggregate_id = e.aggregate_id AND earlier.published_at IS NULL
			  AND (earlier.occurred_at, earlier.event_id) < (e.occurred_at, e.event_id)
		  )
		ORDER BY e.occurred_at, e.event_id
		FOR UPDATE OF e SKIP LOCKED LIMIT $1
	)
	UPDATE outbox_events AS e
	SET lease_owner = $2, lease_until = now() + $3::interval, attempts = attempts + 1
	FROM candidates c WHERE e.event_id = c.event_id
	RETURNING e.event_id, e.event_type, e.aggregate_id, e.event_version,
	          e.correlation_id, e.causation_id, e.payload, e.occurred_at, e.attempts`,
		limit, owner, lease.String())
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	events := make([]application.OutboxEvent, 0)
	for rows.Next() {
		var event application.OutboxEvent
		var causation *string
		var data []byte
		if err := rows.Scan(&event.EventID, &event.EventType, &event.AggregateID, &event.Version,
			&event.CorrelationID, &causation, &data, &event.OccurredAt, &event.Attempts); err != nil {
			return nil, mapError(err)
		}
		if causation != nil {
			event.CausationID = *causation
		}
		event.Data = json.RawMessage(data)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapError(err)
	}
	return events, nil
}

func (r *OutboxDispatcherRepository) MarkPublished(ctx context.Context, eventID, owner string, publishedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE outbox_events
		SET published_at = $3, lease_owner = NULL, lease_until = NULL, last_error = NULL
		WHERE event_id = $1 AND lease_owner = $2 AND published_at IS NULL`, eventID, owner, publishedAt)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return application.ErrOutboxLeaseLost
	}
	return nil
}

func (r *OutboxDispatcherRepository) ScheduleRetry(ctx context.Context, eventID, owner string, nextAttemptAt time.Time, lastError string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE outbox_events
		SET next_attempt_at = $3, lease_owner = NULL, lease_until = NULL, last_error = $4
		WHERE event_id = $1 AND lease_owner = $2 AND published_at IS NULL`, eventID, owner, nextAttemptAt, lastError)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return application.ErrOutboxLeaseLost
	}
	return nil
}

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
var _ application.OutboxDispatchRepository = (*OutboxDispatcherRepository)(nil)
