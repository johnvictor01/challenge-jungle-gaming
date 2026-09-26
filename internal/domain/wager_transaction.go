package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type TransactionKind string

const (
	TransactionOpening  TransactionKind = "OPENING"
	TransactionBet      TransactionKind = "BET"
	TransactionWin      TransactionKind = "WIN"
	TransactionLoss     TransactionKind = "LOSS"
	TransactionRefund   TransactionKind = "REFUND"
	TransactionRollback TransactionKind = "ROLLBACK"
)

type TransactionOrigin string

const (
	TransactionInternal TransactionOrigin = "INTERNAL"
	TransactionExternal TransactionOrigin = "EXTERNAL"
)

type TransactionStatus string

const (
	TransactionPending          TransactionStatus = "PENDING"
	TransactionPendingReference TransactionStatus = "PENDING_REFERENCE"
	TransactionProcessed        TransactionStatus = "PROCESSED"
	TransactionRejected         TransactionStatus = "REJECTED"
	TransactionFailed           TransactionStatus = "FAILED"
)

var (
	ErrInvalidTransaction       = errors.New("invalid wager transaction")
	ErrInvalidTransactionState  = errors.New("invalid wager transaction state")
	ErrTerminalTransaction      = errors.New("terminal transaction cannot change")
	ErrInvalidTransactionAmount = errors.New("invalid amount for transaction type")
	ErrTransactionCurrency      = errors.New("transaction money currency does not match wallet currency")
	ErrInvalidReference         = errors.New("invalid transaction reference")
)

// ExternalWagerInput reúne os campos que chegam do provedor para uma operação.
type ExternalWagerInput struct {
	ID                             string
	Wallet                         *Wallet
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	Kind                           TransactionKind
	Amount                         Money
	RoundID                        string
	GameID                         string
	ReferenceExternalTransactionID string
}

// WagerTransaction representa uma operação interna ou recebida de um provedor.
type WagerTransaction struct {
	ID                             string
	Origin                         TransactionOrigin
	WalletID                       string
	PlayerID                       string
	Currency                       string
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	Kind                           TransactionKind
	Amount                         Money
	RoundID                        string
	GameID                         string
	ReferenceExternalTransactionID string
	ReferenceTransactionID         string
	Status                         TransactionStatus
	FailureCode                    string
	ResultBalance                  *Money
	CreatedAt                      time.Time
	UpdatedAt                      time.Time
	ProcessedAt                    *time.Time
	AttemptCount                   int
	NextAttemptAt                  *time.Time
}

// NewExternalWagerTransaction valida os campos externos e começa em PENDING.
func NewExternalWagerTransaction(input ExternalWagerInput) (*WagerTransaction, error) {
	if input.Wallet == nil {
		return nil, fmt.Errorf("%w: wallet is required", ErrInvalidTransaction)
	}
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Wallet.ID) == "" {
		return nil, fmt.Errorf("%w: transaction and wallet IDs are required", ErrInvalidTransaction)
	}
	if strings.TrimSpace(input.ProviderID) == "" || strings.TrimSpace(input.ExternalTransactionID) == "" ||
		strings.TrimSpace(input.IdempotencyKey) == "" || strings.TrimSpace(input.RoundID) == "" || strings.TrimSpace(input.GameID) == "" {
		return nil, fmt.Errorf("%w: external identifiers are required", ErrInvalidTransaction)
	}
	if !validPayloadHash(input.PayloadHash) {
		return nil, fmt.Errorf("%w: payload hash must be 64 lowercase hexadecimal characters", ErrInvalidTransaction)
	}
	if input.Kind == TransactionOpening || !validExternalKind(input.Kind) {
		return nil, fmt.Errorf("%w: invalid external transaction kind", ErrInvalidTransaction)
	}
	if err := validateTransactionAmount(input.Kind, input.Amount, input.Wallet.Currency); err != nil {
		return nil, err
	}
	if requiresReference(input.Kind) && strings.TrimSpace(input.ReferenceExternalTransactionID) == "" {
		return nil, fmt.Errorf("%w: this operation requires a reference", ErrInvalidTransaction)
	}
	if !allowsReference(input.Kind) && input.ReferenceExternalTransactionID != "" {
		return nil, fmt.Errorf("%w: this operation cannot have a reference", ErrInvalidTransaction)
	}
	if input.Kind == TransactionWin && input.ReferenceExternalTransactionID != "" && strings.TrimSpace(input.ReferenceExternalTransactionID) == "" {
		return nil, fmt.Errorf("%w: reference cannot be blank", ErrInvalidTransaction)
	}

	now := time.Now().UTC()
	return &WagerTransaction{
		ID:                             input.ID,
		Origin:                         TransactionExternal,
		WalletID:                       input.Wallet.ID,
		PlayerID:                       input.Wallet.PlayerID,
		Currency:                       input.Wallet.Currency,
		ProviderID:                     input.ProviderID,
		ExternalTransactionID:          input.ExternalTransactionID,
		IdempotencyKey:                 input.IdempotencyKey,
		PayloadHash:                    input.PayloadHash,
		Kind:                           input.Kind,
		Amount:                         input.Amount,
		RoundID:                        input.RoundID,
		GameID:                         input.GameID,
		ReferenceExternalTransactionID: input.ReferenceExternalTransactionID,
		Status:                         TransactionPending,
		CreatedAt:                      now,
		UpdatedAt:                      now,
	}, nil
}

// NewOpeningWagerTransaction cria a operação interna de abertura com saldo positivo.
func NewOpeningWagerTransaction(id string, wallet *Wallet, amount Money) (*WagerTransaction, error) {
	if wallet == nil || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: transaction ID and wallet are required", ErrInvalidTransaction)
	}
	if amount.Currency != wallet.Currency {
		return nil, ErrTransactionCurrency
	}
	if amount.Units <= 0 {
		return nil, ErrInvalidTransactionAmount
	}
	if wallet.Balance.Units != amount.Units {
		return nil, fmt.Errorf("%w: opening amount must match the initial wallet balance", ErrInvalidTransaction)
	}
	now := time.Now().UTC()
	result := amount
	return &WagerTransaction{
		ID:            id,
		Origin:        TransactionInternal,
		WalletID:      wallet.ID,
		PlayerID:      wallet.PlayerID,
		Currency:      wallet.Currency,
		Kind:          TransactionOpening,
		Amount:        amount,
		Status:        TransactionProcessed,
		ResultBalance: &result,
		CreatedAt:     now,
		UpdatedAt:     now,
		ProcessedAt:   &now,
	}, nil
}

// WaitForReference deixa a operação aguardando uma referência que ainda não chegou.
func (t *WagerTransaction) WaitForReference() error {
	if err := t.ensureNonTerminal(); err != nil {
		return err
	}
	if t.Status != TransactionPending || t.ReferenceExternalTransactionID == "" {
		return ErrInvalidTransactionState
	}
	t.Status = TransactionPendingReference
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// ScheduleReferenceRetry mantém a operação pendente e registra quando o worker
// poderá tentar localizar a referência novamente.
func (t *WagerTransaction) ScheduleReferenceRetry(nextAttemptAt time.Time) error {
	if t == nil || t.Status != TransactionPendingReference || nextAttemptAt.IsZero() {
		return ErrInvalidTransactionState
	}
	t.AttemptCount++
	next := nextAttemptAt.UTC()
	t.NextAttemptAt = &next
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// ResumeAfterReference retoma uma operação cuja referência foi resolvida.
func (t *WagerTransaction) ResumeAfterReference(reference WagerTransaction) error {
	if err := t.ensureNonTerminal(); err != nil {
		return err
	}
	if t.Status != TransactionPendingReference {
		return ErrInvalidTransactionState
	}
	if err := t.validateReference(reference); err != nil {
		return err
	}
	t.ReferenceTransactionID = reference.ID
	t.Status = TransactionPending
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkProcessed conclui com saldo final; os estados terminais não podem ser alterados.
func (t *WagerTransaction) MarkProcessed(resultBalance Money) error {
	if err := t.ensureNonTerminal(); err != nil {
		return err
	}
	if t.Status != TransactionPending {
		return ErrInvalidTransactionState
	}
	if t.ReferenceExternalTransactionID != "" && t.ReferenceTransactionID == "" {
		return ErrInvalidTransactionState
	}
	if resultBalance.Currency != t.Currency {
		return ErrTransactionCurrency
	}
	if resultBalance.Units < 0 {
		return ErrNegativeBalance
	}
	now := time.Now().UTC()
	result := resultBalance
	t.ResultBalance = &result
	t.Status = TransactionProcessed
	t.UpdatedAt = now
	t.ProcessedAt = &now
	return nil
}

// Reject encerra a operação por uma regra de negócio e guarda o código da recusa.
func (t *WagerTransaction) Reject(failureCode string) error {
	return t.finish(TransactionRejected, failureCode)
}

// Fail encerra a operação por uma falha permanente e guarda o código para auditoria.
func (t *WagerTransaction) Fail(failureCode string) error {
	return t.finish(TransactionFailed, failureCode)
}

func (t *WagerTransaction) finish(status TransactionStatus, failureCode string) error {
	if err := t.ensureNonTerminal(); err != nil {
		return err
	}
	if (t.Status != TransactionPending && t.Status != TransactionPendingReference) || strings.TrimSpace(failureCode) == "" {
		return ErrInvalidTransactionState
	}
	now := time.Now().UTC()
	t.Status = status
	t.FailureCode = failureCode
	t.UpdatedAt = now
	t.ProcessedAt = &now
	return nil
}

func (t *WagerTransaction) validateReference(reference WagerTransaction) error {
	if reference.ID == "" || reference.Status != TransactionProcessed || reference.Origin != TransactionExternal ||
		reference.ProviderID != t.ProviderID || reference.PlayerID != t.PlayerID ||
		reference.WalletID != t.WalletID || reference.Currency != t.Currency ||
		reference.RoundID != t.RoundID || reference.ExternalTransactionID != t.ReferenceExternalTransactionID {
		return ErrInvalidReference
	}
	if (t.Kind == TransactionWin && reference.Kind != TransactionBet) ||
		(t.Kind == TransactionRefund && reference.Kind != TransactionBet) ||
		(t.Kind == TransactionRollback && reference.Kind != TransactionBet && reference.Kind != TransactionWin && reference.Kind != TransactionRefund) {
		return ErrInvalidReference
	}
	if (t.Kind == TransactionRefund || t.Kind == TransactionRollback) && t.Amount.Units != reference.Amount.Units {
		return ErrInvalidReference
	}
	return nil
}

func (t *WagerTransaction) ensureNonTerminal() error {
	if t == nil {
		return ErrInvalidTransactionState
	}
	if t.Status == TransactionProcessed || t.Status == TransactionRejected || t.Status == TransactionFailed {
		return ErrTerminalTransaction
	}
	if t.Status != TransactionPending && t.Status != TransactionPendingReference {
		return ErrInvalidTransactionState
	}
	return nil
}

func validateTransactionAmount(kind TransactionKind, amount Money, walletCurrency string) error {
	if amount.Currency != walletCurrency {
		return ErrTransactionCurrency
	}
	if kind == TransactionLoss {
		if amount.Units != 0 {
			return ErrInvalidTransactionAmount
		}
		return nil
	}
	if amount.Units <= 0 {
		return ErrInvalidTransactionAmount
	}
	return nil
}

func validExternalKind(kind TransactionKind) bool {
	return kind == TransactionBet || kind == TransactionWin || kind == TransactionLoss || kind == TransactionRefund || kind == TransactionRollback
}

func requiresReference(kind TransactionKind) bool {
	return kind == TransactionRefund || kind == TransactionRollback
}

func allowsReference(kind TransactionKind) bool {
	return kind == TransactionWin || kind == TransactionRefund || kind == TransactionRollback
}

func validPayloadHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, char := range hash {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
