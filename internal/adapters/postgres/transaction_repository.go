package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type transactionRepository struct{ tx pgx.Tx }

const transactionColumns = `id, origin, wallet_id, player_id, currency, provider_id,
    external_transaction_id, idempotency_key, payload_hash, kind, amount_minor,
    round_id, game_id, reference_external_transaction_id, reference_transaction_id,
    status, failure_code, result_balance_minor, result_currency, attempt_count,
    next_attempt_at, created_at, updated_at, processed_at`

func (r transactionRepository) FindByID(ctx context.Context, id string) (*domain.WagerTransaction, error) {
	return scanTransaction(r.tx.QueryRow(ctx, "SELECT "+transactionColumns+" FROM wager_transactions WHERE id = $1", id))
}

func (r transactionRepository) FindByIDForUpdate(ctx context.Context, id string) (*domain.WagerTransaction, error) {
	return scanTransaction(r.tx.QueryRow(ctx, "SELECT "+transactionColumns+" FROM wager_transactions WHERE id = $1 FOR UPDATE", id))
}

func (r transactionRepository) FindByIdempotencyKey(ctx context.Context, providerID, key string) (*domain.WagerTransaction, error) {
	return scanTransaction(r.tx.QueryRow(ctx, "SELECT "+transactionColumns+" FROM wager_transactions WHERE provider_id = $1 AND idempotency_key = $2 AND origin = 'EXTERNAL'", providerID, key))
}

func (r transactionRepository) FindByExternalID(ctx context.Context, providerID, externalID string) (*domain.WagerTransaction, error) {
	return scanTransaction(r.tx.QueryRow(ctx, "SELECT "+transactionColumns+" FROM wager_transactions WHERE provider_id = $1 AND external_transaction_id = $2 AND origin = 'EXTERNAL'", providerID, externalID))
}

func (r transactionRepository) FindSuccessfulReversal(ctx context.Context, referenceTransactionID string) (*domain.WagerTransaction, error) {
	return scanTransaction(r.tx.QueryRow(ctx, `SELECT `+transactionColumns+` FROM wager_transactions
        WHERE reference_transaction_id = $1 AND status = 'PROCESSED' AND kind IN ('REFUND', 'ROLLBACK')`, referenceTransactionID))
}

func (r transactionRepository) Create(ctx context.Context, transaction *domain.WagerTransaction) error {
	_, err := r.tx.Exec(ctx, `INSERT INTO wager_transactions (`+transactionColumns+`)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`, transactionArgs(transaction)...)
	return mapError(err)
}

func (r transactionRepository) Update(ctx context.Context, transaction *domain.WagerTransaction) error {
	_, err := r.tx.Exec(ctx, `UPDATE wager_transactions SET
        reference_transaction_id = $2, status = $3, failure_code = $4,
        result_balance_minor = $5, result_currency = $6, attempt_count = $7,
        next_attempt_at = $8, updated_at = $9, processed_at = $10
        WHERE id = $1`,
		transaction.ID(), nullableString(transaction.ReferenceTransactionID()), string(transaction.Status()), nullableString(transaction.FailureCode()),
		resultBalance(transaction), resultCurrency(transaction), transaction.AttemptCount(), transaction.NextAttemptAt(),
		transaction.UpdatedAt(), transaction.ProcessedAt())
	return mapError(err)
}

func transactionArgs(transaction *domain.WagerTransaction) []any {
	return []any{
		transaction.ID(), string(transaction.Origin()), transaction.WalletID(), transaction.PlayerID(), transaction.Currency(),
		nullableString(transaction.ProviderID()), nullableString(transaction.ExternalTransactionID()), nullableString(transaction.IdempotencyKey()), nullableString(transaction.PayloadHash()),
		string(transaction.Kind()), transaction.Amount().Units(), nullableString(transaction.RoundID()), nullableString(transaction.GameID()),
		nullableString(transaction.ReferenceExternalTransactionID()), nullableString(transaction.ReferenceTransactionID()), string(transaction.Status()), nullableString(transaction.FailureCode()),
		resultBalance(transaction), resultCurrency(transaction), transaction.AttemptCount(), transaction.NextAttemptAt(),
		transaction.CreatedAt(), transaction.UpdatedAt(), transaction.ProcessedAt(),
	}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func resultBalance(transaction *domain.WagerTransaction) any {
	if balance := transaction.ResultBalance(); balance == nil {
		return nil
	} else {
		return balance.Units()
	}
}

func resultCurrency(transaction *domain.WagerTransaction) any {
	if balance := transaction.ResultBalance(); balance == nil {
		return nil
	} else {
		return balance.Currency()
	}
}

func scanTransaction(row rowScanner) (*domain.WagerTransaction, error) {
	var id, walletID, playerID, currency string
	var origin, kind, status string
	var providerID, externalID, idempotencyKey, payloadHash *string
	var roundID, gameID, referenceExternalID, referenceID, failureCode *string
	var amountMinor int64
	var attemptCount int
	var createdAt, updatedAt time.Time
	var resultBalance *int64
	var resultCurrency *string
	var nextAttemptAt, processedAt *time.Time
	if err := row.Scan(&id, &origin, &walletID, &playerID, &currency,
		&providerID, &externalID, &idempotencyKey, &payloadHash, &kind, &amountMinor,
		&roundID, &gameID, &referenceExternalID, &referenceID, &status, &failureCode, &resultBalance,
		&resultCurrency, &attemptCount, &nextAttemptAt, &createdAt, &updatedAt, &processedAt); err != nil {
		return nil, mapError(err)
	}
	amount, err := domain.NewMoney(amountMinor, currency)
	if err != nil {
		return nil, err
	}
	var result *domain.Money
	if resultBalance != nil {
		if resultCurrency == nil {
			return nil, fmt.Errorf("transaction %s has result without currency", id)
		}
		money, err := domain.NewMoney(*resultBalance, *resultCurrency)
		if err != nil {
			return nil, err
		}
		result = &money
	}
	return domain.RehydrateWagerTransaction(domain.WagerTransactionState{
		ID: id, Origin: domain.TransactionOrigin(origin), WalletID: walletID, PlayerID: playerID, Currency: currency,
		ProviderID: valueOf(providerID), ExternalTransactionID: valueOf(externalID), IdempotencyKey: valueOf(idempotencyKey), PayloadHash: valueOf(payloadHash),
		Kind: domain.TransactionKind(kind), Amount: amount, RoundID: valueOf(roundID), GameID: valueOf(gameID),
		ReferenceExternalTransactionID: valueOf(referenceExternalID), ReferenceTransactionID: valueOf(referenceID),
		Status: domain.TransactionStatus(status), FailureCode: valueOf(failureCode), ResultBalance: result,
		CreatedAt: createdAt, UpdatedAt: updatedAt, ProcessedAt: processedAt, AttemptCount: attemptCount, NextAttemptAt: nextAttemptAt,
	})
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var _ application.WagerTransactionRepository = transactionRepository{}
