package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	xcurrency "golang.org/x/text/currency"
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
	id        string
	playerID  string
	currency  string
	balance   Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

func (w *Wallet) ID() string               { return w.id }
func (w *Wallet) PlayerID() string         { return w.playerID }
func (w *Wallet) Currency() string         { return w.currency }
func (w *Wallet) Balance() Money           { return w.balance }
func (w *Wallet) Version() int64           { return w.version }
func (w *Wallet) CreatedAt() time.Time     { return w.createdAt }
func (w *Wallet) UpdatedAt() time.Time     { return w.updatedAt }

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
		id:        id,
		playerID:  playerID,
		currency:  currency,
		balance:   Money{units: initialBalance, currency: currency},
		version:   1,
		createdAt: now,
		updatedAt: now,
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
	if !validCurrency(balance.Currency()) {
		return nil, ErrInvalidCurrency
	}
	if balance.Units() < 0 {
		return nil, ErrNegativeBalance
	}
	if version < 1 {
		return nil, ErrInvalidVersion
	}
	if createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return nil, ErrInvalidTimestamps
	}

	return &Wallet{
		id:        id,
		playerID:  playerID,
		currency:  balance.Currency(),
		balance:   balance,
		version:   version,
		createdAt: createdAt.UTC(),
		updatedAt: updatedAt.UTC(),
	}, nil
}

// Credit adiciona valor à carteira; saldo e versão mudam somente se o valor for positivo.
func (w *Wallet) Credit(amount Money) error {
	if err := w.validateMovement(amount); err != nil {
		return err
	}
	if amount.Units() == 0 {
		return nil
	}
	newBalance, err := w.balance.Add(amount)
	if err != nil {
		return err
	}
	if err := w.advanceVersion(); err != nil {
		return err
	}
	w.balance = newBalance
	w.updatedAt = time.Now().UTC()
	return nil
}

// Debit remove valor da carteira sem permitir saldo negativo.
func (w *Wallet) Debit(amount Money) error {
	if err := w.validateMovement(amount); err != nil {
		return err
	}
	if amount.Units() == 0 {
		return nil
	}
	if amount.Units() > w.balance.Units() {
		return ErrInsufficientFunds
	}
	newBalance, err := w.balance.Subtract(amount)
	if err != nil {
		return err
	}
	if newBalance.Units() < 0 {
		return ErrInsufficientFunds
	}
	if err := w.advanceVersion(); err != nil {
		return err
	}
	w.balance = newBalance
	w.updatedAt = time.Now().UTC()
	return nil
}

func (w *Wallet) validateMovement(amount Money) error {
	if w == nil {
		return errors.New("wallet cannot be nil")
	}
	if amount.Currency() != w.currency {
		return ErrCurrencyMismatch
	}
	if amount.Units() < 0 {
		return ErrInvalidAmount
	}
	return nil
}

func (w *Wallet) advanceVersion() error {
	if w.version == int64(^uint64(0)>>1) {
		return ErrVersionOverflow
	}
	w.version++
	return nil
}

func validCurrency(currency string) bool {
	if len(currency) != 3 || currency != strings.ToUpper(currency) {
		return false
	}
	_, err := xcurrency.ParseISO(currency)
	return err == nil
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
