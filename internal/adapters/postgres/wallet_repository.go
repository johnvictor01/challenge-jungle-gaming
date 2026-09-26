package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type walletRepository struct{ tx pgx.Tx }

const walletColumns = "id, player_id, currency, balance_minor, version, created_at, updated_at"

func (r walletRepository) FindByID(ctx context.Context, id string) (*domain.Wallet, error) {
	row := r.tx.QueryRow(ctx, "SELECT "+walletColumns+" FROM wallets WHERE id = $1 FOR UPDATE", id)
	return scanWallet(row)
}

func (r walletRepository) FindByPlayerAndCurrency(ctx context.Context, playerID, currency string) (*domain.Wallet, error) {
	row := r.tx.QueryRow(ctx, "SELECT "+walletColumns+" FROM wallets WHERE player_id = $1 AND currency = $2", playerID, currency)
	return scanWallet(row)
}

func (r walletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	_, err := r.tx.Exec(ctx, `INSERT INTO wallets
        (id, player_id, currency, balance_minor, version, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		wallet.ID(), wallet.PlayerID(), wallet.Currency(), wallet.Balance().Units(), wallet.Version(), wallet.CreatedAt(), wallet.UpdatedAt())
	return mapError(err)
}

func (r walletRepository) Update(ctx context.Context, wallet *domain.Wallet, expectedVersion int64) error {
	command, err := r.tx.Exec(ctx, `UPDATE wallets
        SET balance_minor = $1, version = $2, updated_at = $3
        WHERE id = $4 AND version = $5`,
		wallet.Balance().Units(), wallet.Version(), wallet.UpdatedAt(), wallet.ID(), expectedVersion)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() != 1 {
		return application.ErrPersistenceConflict
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanWallet(row rowScanner) (*domain.Wallet, error) {
	var id, playerID, currency string
	var balance, version int64
	var created, updated time.Time
	if err := row.Scan(&id, &playerID, &currency, &balance, &version, &created, &updated); err != nil {
		return nil, mapError(err)
	}
	money, err := domain.NewMoney(balance, currency)
	if err != nil {
		return nil, err
	}
	wallet, err := domain.RehydrateWallet(id, playerID, money, version, created, updated)
	if err != nil {
		return nil, fmt.Errorf("rehydrate wallet: %w", err)
	}
	return wallet, nil
}
