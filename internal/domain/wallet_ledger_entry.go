package domain

import (
	"errors"
	"strings"
	"time"
)

type Direction string

const (
	DirectionDebit  Direction = "DEBIT"
	DirectionCredit Direction = "CREDIT"
)

var (
	ErrInvalidLedgerEntry     = errors.New("invalid wallet ledger entry")
	ErrInvalidLedgerDirection = errors.New("ledger direction must be DEBIT or CREDIT")
	ErrInvalidLedgerAmount    = errors.New("ledger amount must be positive")
	ErrInvalidLedgerIDs       = errors.New("ledger wallet and transaction IDs are required")
	ErrLedgerBalanceMismatch  = errors.New("ledger balance calculation does not match")
	ErrInvalidLedgerTimestamp = errors.New("ledger creation timestamp is required")
)

// WalletLedgerEntry representa uma mudança de saldo já confirmada.
// Os valores usam Money para manter a moeda junto dos centavos.
type WalletLedgerEntry struct {
	ID            string
	WalletID      string
	TransactionID string
	Direction     Direction
	Amount        Money
	BalanceBefore Money
	BalanceAfter  Money
	CreatedAt     time.Time
}

// NewWalletLedgerEntry cria um lançamento e valida a conta do saldo.
func NewWalletLedgerEntry(id, walletID, transactionID string, direction Direction, amount, balanceBefore, balanceAfter Money) (*WalletLedgerEntry, error) {
	if err := validateWalletLedgerEntry(id, walletID, transactionID, direction, amount, balanceBefore, balanceAfter); err != nil {
		return nil, err
	}
	return &WalletLedgerEntry{
		ID:            id,
		WalletID:      walletID,
		TransactionID: transactionID,
		Direction:     direction,
		Amount:        amount,
		BalanceBefore: balanceBefore,
		BalanceAfter:  balanceAfter,
		CreatedAt:     time.Now().UTC(),
	}, nil
}

// RehydrateWalletLedgerEntry recupera um lançamento persistido sem reaplicar a movimentação.
func RehydrateWalletLedgerEntry(id, walletID, transactionID string, direction Direction, amount, balanceBefore, balanceAfter Money, createdAt time.Time) (*WalletLedgerEntry, error) {
	if err := validateWalletLedgerEntry(id, walletID, transactionID, direction, amount, balanceBefore, balanceAfter); err != nil {
		return nil, err
	}
	if createdAt.IsZero() {
		return nil, ErrInvalidLedgerTimestamp
	}
	return &WalletLedgerEntry{
		ID:            id,
		WalletID:      walletID,
		TransactionID: transactionID,
		Direction:     direction,
		Amount:        amount,
		BalanceBefore: balanceBefore,
		BalanceAfter:  balanceAfter,
		CreatedAt:     createdAt.UTC(),
	}, nil
}

func validateWalletLedgerEntry(id, walletID, transactionID string, direction Direction, amount, balanceBefore, balanceAfter Money) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(walletID) == "" || strings.TrimSpace(transactionID) == "" {
		return ErrInvalidLedgerIDs
	}
	if direction != DirectionDebit && direction != DirectionCredit {
		return ErrInvalidLedgerDirection
	}
	if amount.Units <= 0 {
		return ErrInvalidLedgerAmount
	}
	if !validCurrency(amount.Currency) || amount.Currency != balanceBefore.Currency || amount.Currency != balanceAfter.Currency {
		return ErrCurrencyMismatch
	}
	if balanceBefore.Units < 0 || balanceAfter.Units < 0 {
		return ErrNegativeBalance
	}

	var expected Money
	var err error
	if direction == DirectionCredit {
		expected, err = balanceBefore.Add(amount)
	} else {
		if amount.Units > balanceBefore.Units {
			return ErrInsufficientFunds
		}
		expected, err = balanceBefore.Subtract(amount)
	}
	if err != nil {
		return err
	}
	if expected != balanceAfter {
		return ErrLedgerBalanceMismatch
	}
	return nil
}
