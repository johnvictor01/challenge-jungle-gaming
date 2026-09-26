package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
)

// Store abre transações PostgreSQL e entrega repositórios ligados ao mesmo tx.
type Store struct {
	pool *pgxpool.Pool
}

func NewPool(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	return s.pool.Ping(ctx)
}

// WithinTransaction usa isolamento serializável. O lock da carteira em
// FindByID ordena escritores da mesma carteira; o isolamento também protege
// decisões que leem referências e idempotência na mesma operação.
func (s *Store) WithinTransaction(ctx context.Context, callback func(application.Repositories) error) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	for attempt := 0; attempt < 3; attempt++ {
		tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		repositories := application.Repositories{
			Wallets:      walletRepository{tx: tx},
			Transactions: transactionRepository{tx: tx},
			Ledger:       ledgerRepository{tx: tx},
			Outbox:       outboxRepository{tx: tx},
			Inbox:        inboxRepository{tx: tx},
		}
		err = callback(repositories)
		if err != nil {
			_ = tx.Rollback(ctx)
			if isRetryableTransactionError(err) && attempt < 2 {
				continue
			}
			return err
		}
		err = tx.Commit(ctx)
		if err == nil {
			return nil
		}
		_ = tx.Rollback(ctx)
		if !isRetryableTransactionError(err) || attempt == 2 {
			return mapError(err)
		}
	}
	return errors.New("unreachable postgres transaction retry")
}

func (s *Store) WithinInboxTransaction(ctx context.Context, consumerName, messageID, payloadHash string, callback func(application.Repositories, bool) error) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	for attempt := 0; attempt < 3; attempt++ {
		tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return fmt.Errorf("begin inbox transaction: %w", err)
		}
		result, err := tx.Exec(ctx, `INSERT INTO inbox_messages (consumer_name, message_id, payload_hash)
			VALUES ($1,$2,$3) ON CONFLICT (consumer_name, message_id) DO NOTHING`, consumerName, messageID, payloadHash)
		if err != nil {
			_ = tx.Rollback(ctx)
			if isRetryableTransactionError(err) && attempt < 2 {
				continue
			}
			return mapError(err)
		}
		duplicate := result.RowsAffected() == 0
		repositories := application.Repositories{
			Wallets: walletRepository{tx: tx}, Transactions: transactionRepository{tx: tx},
			Ledger: ledgerRepository{tx: tx}, Outbox: outboxRepository{tx: tx},
			Inbox: inboxRepository{tx: tx},
		}
		if err := callback(repositories, duplicate); err != nil {
			_ = tx.Rollback(ctx)
			if isRetryableTransactionError(err) && attempt < 2 {
				continue
			}
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
			if isRetryableTransactionError(err) && attempt < 2 {
				continue
			}
			return mapError(err)
		}
		return nil
	}
	return errors.New("inbox transaction retry limit reached")
}

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return application.ErrPersistenceConflict
	}
	return err
}

func isRetryableTransactionError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}
