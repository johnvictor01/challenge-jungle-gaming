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
	id            string
	walletID      string
	transactionID string
	direction     Direction
	amount        Money
	balanceBefore Money
	balanceAfter  Money
	createdAt     time.Time
}

func (e *WalletLedgerEntry) ID() string { return e.id }
func (e *WalletLedgerEntry) WalletID() string { return e.walletID }
func (e *WalletLedgerEntry) TransactionID() string { return e.transactionID }
func (e *WalletLedgerEntry) Direction() Direction { return e.direction }
func (e *WalletLedgerEntry) Amount() Money { return e.amount }
func (e *WalletLedgerEntry) BalanceBefore() Money { return e.balanceBefore }
func (e *WalletLedgerEntry) BalanceAfter() Money { return e.balanceAfter }
func (e *WalletLedgerEntry) CreatedAt() time.Time { return e.createdAt }

// NewWalletLedgerEntry cria um lançamento e valida a conta do saldo.
func NewWalletLedgerEntry(id, walletID, transactionID string, direction Direction, amount, balanceBefore, balanceAfter Money) (*WalletLedgerEntry, error) {
	if err := validateWalletLedgerEntry(id, walletID, transactionID, direction, amount, balanceBefore, balanceAfter); err != nil {
		return nil, err
	}
	return &WalletLedgerEntry{
		id: id, walletID: walletID, transactionID: transactionID, direction: direction,
		amount: amount, balanceBefore: balanceBefore, balanceAfter: balanceAfter, createdAt: time.Now().UTC(),
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
		id: id, walletID: walletID, transactionID: transactionID, direction: direction,
		amount: amount, balanceBefore: balanceBefore, balanceAfter: balanceAfter, createdAt: createdAt.UTC(),
	}, nil
}

func validateWalletLedgerEntry(id, walletID, transactionID string, direction Direction, amount, balanceBefore, balanceAfter Money) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(walletID) == "" || strings.TrimSpace(transactionID) == "" {
		return ErrInvalidLedgerIDs
	}
	if direction != DirectionDebit && direction != DirectionCredit {
		return ErrInvalidLedgerDirection
	}
	if amount.Units() <= 0 {
		return ErrInvalidLedgerAmount
	}
	if !validCurrency(amount.Currency()) || amount.Currency() != balanceBefore.Currency() || amount.Currency() != balanceAfter.Currency() {
		return ErrCurrencyMismatch
	}
	if balanceBefore.Units() < 0 || balanceAfter.Units() < 0 {
		return ErrNegativeBalance
	}

	var expected Money
	var err error
	if direction == DirectionCredit {
		expected, err = balanceBefore.Add(amount)
	} else {
		if amount.Units() > balanceBefore.Units() {
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
