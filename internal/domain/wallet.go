package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidPlayerID   = errors.New("player ID cannot be empty")
	ErrInvalidWalletID   = errors.New("wallet ID cannot be empty")
	ErrInvalidCurrency   = errors.New("currency must be a three-letter uppercase code")
	ErrNegativeBalance   = errors.New("wallet balance cannot be negative")
	ErrInvalidAmount     = errors.New("wallet movement amount must not be negative")
	ErrInsufficientFunds = errors.New("wallet has insufficient funds")
	ErrInvalidVersion    = errors.New("wallet version must be at least 1")
	ErrInvalidTimestamps = errors.New("wallet timestamps are invalid")
	ErrVersionOverflow   = errors.New("wallet version overflow")
)

// Wallet guarda identidade, jogador, moeda, saldo, versão e datas da carteira.
type Wallet struct {
	ID        string
	PlayerID  string
	Currency  string
	Balance   Money
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewWallet cria uma carteira com ID novo, saldo inicial e versão 1.
func NewWallet(playerID string, currency string, initialBalance int64) (*Wallet, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, ErrInvalidPlayerID
	}
	if !validCurrency(currency) {
		return nil, ErrInvalidCurrency
	}
	if initialBalance < 0 {
		return nil, ErrNegativeBalance
	}

	id, err := newWalletID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Wallet{
		ID:        id,
		PlayerID:  playerID,
		Currency:  currency,
		Balance:   Money{Units: initialBalance, Currency: currency},
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// RehydrateWallet monta uma carteira que já existe no banco sem aplicar movimentações.
func RehydrateWallet(id string, playerID string, balance Money, version int64, createdAt time.Time, updatedAt time.Time) (*Wallet, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrInvalidWalletID
	}
	if strings.TrimSpace(playerID) == "" {
		return nil, ErrInvalidPlayerID
	}
	if !validCurrency(balance.Currency) {
		return nil, ErrInvalidCurrency
	}
	if balance.Units < 0 {
		return nil, ErrNegativeBalance
	}
	if version < 1 {
		return nil, ErrInvalidVersion
	}
	if createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return nil, ErrInvalidTimestamps
	}

	return &Wallet{
		ID:        id,
		PlayerID:  playerID,
		Currency:  balance.Currency,
		Balance:   balance,
		Version:   version,
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
	}, nil
}

// Credit adiciona valor à carteira; saldo e versão mudam somente se o valor for positivo.
func (w *Wallet) Credit(amount Money) error {
	if err := w.validateMovement(amount); err != nil {
		return err
	}
	if amount.Units == 0 {
		return nil
	}
	newBalance, err := w.Balance.Add(amount)
	if err != nil {
		return err
	}
	if err := w.advanceVersion(); err != nil {
		return err
	}
	w.Balance = newBalance
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// Debit remove valor da carteira sem permitir saldo negativo.
func (w *Wallet) Debit(amount Money) error {
	if err := w.validateMovement(amount); err != nil {
		return err
	}
	if amount.Units == 0 {
		return nil
	}
	if amount.Units > w.Balance.Units {
		return ErrInsufficientFunds
	}
	newBalance, err := w.Balance.Subtract(amount)
	if err != nil {
		return err
	}
	if newBalance.Units < 0 {
		return ErrInsufficientFunds
	}
	if err := w.advanceVersion(); err != nil {
		return err
	}
	w.Balance = newBalance
	w.UpdatedAt = time.Now().UTC()
	return nil
}

func (w *Wallet) validateMovement(amount Money) error {
	if w == nil {
		return errors.New("wallet cannot be nil")
	}
	if amount.Currency != w.Currency {
		return ErrCurrencyMismatch
	}
	if amount.Units < 0 {
		return ErrInvalidAmount
	}
	return nil
}

func (w *Wallet) advanceVersion() error {
	if w.Version == int64(^uint64(0)>>1) {
		return ErrVersionOverflow
	}
	w.Version++
	return nil
}

func validCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return false
		}
	}
	return true
}

func newWalletID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40 // UUID versão 4
	id[8] = (id[8] & 0x3f) | 0x80 // variante RFC 4122
	encoded := hex.EncodeToString(id[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
