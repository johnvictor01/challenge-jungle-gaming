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
	id                             string
	origin                         TransactionOrigin
	walletID                       string
	playerID                       string
	currency                       string
	providerID                     string
	externalTransactionID          string
	idempotencyKey                 string
	payloadHash                    string
	kind                           TransactionKind
	amount                         Money
	roundID                        string
	gameID                         string
	referenceExternalTransactionID string
	referenceTransactionID         string
	status                         TransactionStatus
	failureCode                    string
	resultBalance                  *Money
	createdAt                      time.Time
	updatedAt                      time.Time
	processedAt                    *time.Time
	attemptCount                   int
	nextAttemptAt                  *time.Time
}

func (t *WagerTransaction) ID() string                    { return t.id }
func (t *WagerTransaction) Origin() TransactionOrigin     { return t.origin }
func (t *WagerTransaction) WalletID() string              { return t.walletID }
func (t *WagerTransaction) PlayerID() string              { return t.playerID }
func (t *WagerTransaction) Currency() string              { return t.currency }
func (t *WagerTransaction) ProviderID() string            { return t.providerID }
func (t *WagerTransaction) ExternalTransactionID() string { return t.externalTransactionID }
func (t *WagerTransaction) IdempotencyKey() string        { return t.idempotencyKey }
func (t *WagerTransaction) PayloadHash() string           { return t.payloadHash }
func (t *WagerTransaction) Kind() TransactionKind         { return t.kind }
func (t *WagerTransaction) Amount() Money                 { return t.amount }
func (t *WagerTransaction) RoundID() string               { return t.roundID }
func (t *WagerTransaction) GameID() string                { return t.gameID }
func (t *WagerTransaction) ReferenceExternalTransactionID() string {
	return t.referenceExternalTransactionID
}
func (t *WagerTransaction) ReferenceTransactionID() string { return t.referenceTransactionID }
func (t *WagerTransaction) Status() TransactionStatus      { return t.status }
func (t *WagerTransaction) FailureCode() string            { return t.failureCode }
func (t *WagerTransaction) ResultBalance() *Money {
	if t.resultBalance == nil {
		return nil
	}
	copy := *t.resultBalance
	return &copy
}
func (t *WagerTransaction) CreatedAt() time.Time { return t.createdAt }
func (t *WagerTransaction) UpdatedAt() time.Time { return t.updatedAt }
func (t *WagerTransaction) ProcessedAt() *time.Time {
	if t.processedAt == nil {
		return nil
	}
	copy := *t.processedAt
	return &copy
}
func (t *WagerTransaction) AttemptCount() int { return t.attemptCount }
func (t *WagerTransaction) NextAttemptAt() *time.Time {
	if t.nextAttemptAt == nil {
		return nil
	}
	copy := *t.nextAttemptAt
	return &copy
}

// Clone devolve uma cópia independente da entidade para repositórios e testes.
func (t *WagerTransaction) Clone() *WagerTransaction {
	if t == nil {
		return nil
	}
	copy := *t
	if t.resultBalance != nil {
		balance := *t.resultBalance
		copy.resultBalance = &balance
	}
	if t.processedAt != nil {
		value := *t.processedAt
		copy.processedAt = &value
	}
	if t.nextAttemptAt != nil {
		value := *t.nextAttemptAt
		copy.nextAttemptAt = &value
	}
	return &copy
}

// State cria um snapshot serializável para persistência e fixtures externas ao domínio.
func (t *WagerTransaction) State() WagerTransactionState {
	if t == nil {
		return WagerTransactionState{}
	}
	return WagerTransactionState{
		ID: t.id, WalletID: t.walletID, PlayerID: t.playerID, Currency: t.currency,
		ProviderID: t.providerID, ExternalTransactionID: t.externalTransactionID, IdempotencyKey: t.idempotencyKey, PayloadHash: t.payloadHash,
		RoundID: t.roundID, GameID: t.gameID, ReferenceExternalTransactionID: t.referenceExternalTransactionID, ReferenceTransactionID: t.referenceTransactionID,
		Origin: t.origin, Kind: t.kind, Amount: t.amount, Status: t.status, FailureCode: t.failureCode,
		ResultBalance: t.ResultBalance(), CreatedAt: t.createdAt, UpdatedAt: t.updatedAt, ProcessedAt: t.ProcessedAt(),
		AttemptCount: t.attemptCount, NextAttemptAt: t.NextAttemptAt(),
	}
}

// WagerTransactionState é um snapshot para reconstrução a partir da persistência.
type WagerTransactionState struct {
	ID, WalletID, PlayerID, Currency                                        string
	ProviderID, ExternalTransactionID, IdempotencyKey, PayloadHash          string
	RoundID, GameID, ReferenceExternalTransactionID, ReferenceTransactionID string
	Origin                                                                  TransactionOrigin
	Kind                                                                    TransactionKind
	Amount                                                                  Money
	Status                                                                  TransactionStatus
	FailureCode                                                             string
	ResultBalance                                                           *Money
	CreatedAt, UpdatedAt                                                    time.Time
	ProcessedAt                                                             *time.Time
	AttemptCount                                                            int
	NextAttemptAt                                                           *time.Time
}

// RehydrateWagerTransaction reconstrói uma operação persistida sem reaplicar efeitos.
func RehydrateWagerTransaction(state WagerTransactionState) (*WagerTransaction, error) {
	if strings.TrimSpace(state.ID) == "" || strings.TrimSpace(state.WalletID) == "" || strings.TrimSpace(state.PlayerID) == "" {
		return nil, fmt.Errorf("%w: transaction, wallet and player IDs are required", ErrInvalidTransaction)
	}
	if !validCurrency(state.Currency) || state.Amount.Currency() != state.Currency {
		return nil, ErrTransactionCurrency
	}
	if state.CreatedAt.IsZero() || state.UpdatedAt.IsZero() || state.UpdatedAt.Before(state.CreatedAt) || state.AttemptCount < 0 {
		return nil, fmt.Errorf("%w: persisted timestamps or attempt count are invalid", ErrInvalidTransaction)
	}
	if state.Origin != TransactionInternal && state.Origin != TransactionExternal {
		return nil, fmt.Errorf("%w: unknown origin", ErrInvalidTransaction)
	}
	switch state.Status {
	case TransactionPending, TransactionPendingReference, TransactionProcessed, TransactionRejected, TransactionFailed:
	default:
		return nil, fmt.Errorf("%w: unknown status", ErrInvalidTransaction)
	}
	if state.Status == TransactionPendingReference && strings.TrimSpace(state.ReferenceExternalTransactionID) == "" {
		return nil, fmt.Errorf("%w: pending reference is required", ErrInvalidTransaction)
	}
	if (state.Status == TransactionRejected || state.Status == TransactionFailed) && strings.TrimSpace(state.FailureCode) == "" {
		return nil, fmt.Errorf("%w: terminal failure code is required", ErrInvalidTransaction)
	}
	if (state.Status == TransactionProcessed || state.Status == TransactionRejected || state.Status == TransactionFailed) && state.ProcessedAt == nil {
		return nil, fmt.Errorf("%w: terminal timestamp is required", ErrInvalidTransaction)
	}
	if state.ResultBalance != nil && (state.ResultBalance.Currency() != state.Currency || state.ResultBalance.Units() < 0) {
		return nil, ErrTransactionCurrency
	}
	if state.Origin == TransactionExternal {
		if !validExternalKind(state.Kind) || strings.TrimSpace(state.ProviderID) == "" || strings.TrimSpace(state.ExternalTransactionID) == "" || strings.TrimSpace(state.IdempotencyKey) == "" || !validPayloadHash(state.PayloadHash) {
			return nil, fmt.Errorf("%w: external transaction metadata is invalid", ErrInvalidTransaction)
		}
		if err := validateTransactionAmount(state.Kind, state.Amount, state.Currency); err != nil {
			return nil, err
		}
		if requiresReference(state.Kind) && strings.TrimSpace(state.ReferenceExternalTransactionID) == "" {
			return nil, ErrInvalidReference
		}
	} else if state.Kind != TransactionOpening || state.Amount.Units() <= 0 || state.ProviderID != "" || state.ExternalTransactionID != "" || state.IdempotencyKey != "" || state.PayloadHash != "" {
		return nil, fmt.Errorf("%w: internal transaction must be an opening without external metadata", ErrInvalidTransaction)
	}
	copyTime := func(value *time.Time) *time.Time {
		if value == nil {
			return nil
		}
		copy := value.UTC()
		return &copy
	}
	var result *Money
	if state.ResultBalance != nil {
		copy := *state.ResultBalance
		result = &copy
	}
	return &WagerTransaction{
		id: state.ID, origin: state.Origin, walletID: state.WalletID, playerID: state.PlayerID, currency: state.Currency,
		providerID: state.ProviderID, externalTransactionID: state.ExternalTransactionID, idempotencyKey: state.IdempotencyKey,
		payloadHash: state.PayloadHash, kind: state.Kind, amount: state.Amount, roundID: state.RoundID, gameID: state.GameID,
		referenceExternalTransactionID: state.ReferenceExternalTransactionID, referenceTransactionID: state.ReferenceTransactionID,
		status: state.Status, failureCode: state.FailureCode, resultBalance: result, createdAt: state.CreatedAt.UTC(),
		updatedAt: state.UpdatedAt.UTC(), processedAt: copyTime(state.ProcessedAt), attemptCount: state.AttemptCount,
		nextAttemptAt: copyTime(state.NextAttemptAt),
	}, nil
}

// NewExternalWagerTransaction valida os campos externos e começa em PENDING.
func NewExternalWagerTransaction(input ExternalWagerInput) (*WagerTransaction, error) {
	if input.Wallet == nil {
		return nil, fmt.Errorf("%w: wallet is required", ErrInvalidTransaction)
	}
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Wallet.ID()) == "" {
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
	if err := validateTransactionAmount(input.Kind, input.Amount, input.Wallet.Currency()); err != nil {
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
		id: input.ID, origin: TransactionExternal, walletID: input.Wallet.ID(), playerID: input.Wallet.PlayerID(), currency: input.Wallet.Currency(),
		providerID: input.ProviderID, externalTransactionID: input.ExternalTransactionID, idempotencyKey: input.IdempotencyKey,
		payloadHash: input.PayloadHash, kind: input.Kind, amount: input.Amount, roundID: input.RoundID, gameID: input.GameID,
		referenceExternalTransactionID: input.ReferenceExternalTransactionID, status: TransactionPending, createdAt: now, updatedAt: now,
	}, nil
}

// NewOpeningWagerTransaction cria a operação interna de abertura com saldo positivo.
func NewOpeningWagerTransaction(id string, wallet *Wallet, amount Money) (*WagerTransaction, error) {
	if wallet == nil || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: transaction ID and wallet are required", ErrInvalidTransaction)
	}
	if amount.Currency() != wallet.Currency() {
		return nil, ErrTransactionCurrency
	}
	if amount.Units() <= 0 {
		return nil, ErrInvalidTransactionAmount
	}
	if wallet.Balance().Units() != amount.Units() {
		return nil, fmt.Errorf("%w: opening amount must match the initial wallet balance", ErrInvalidTransaction)
	}
	now := time.Now().UTC()
	result := amount
	return &WagerTransaction{
		id: id, origin: TransactionInternal, walletID: wallet.ID(), playerID: wallet.PlayerID(), currency: wallet.Currency(),
		kind: TransactionOpening, amount: amount, status: TransactionProcessed, resultBalance: &result,
		createdAt: now, updatedAt: now, processedAt: &now,
	}, nil
}

// WaitForReference deixa a operação aguardando uma referência que ainda não chegou.
func (t *WagerTransaction) WaitForReference() error {
	if err := t.ensureNonTerminal(); err != nil {
		return err
	}
	if t.status != TransactionPending || t.referenceExternalTransactionID == "" {
		return ErrInvalidTransactionState
	}
	t.status = TransactionPendingReference
	t.updatedAt = time.Now().UTC()
	return nil
}

// ScheduleReferenceRetry mantém a operação pendente e registra quando o worker
// poderá tentar localizar a referência novamente.
func (t *WagerTransaction) ScheduleReferenceRetry(nextAttemptAt time.Time) error {
	if t == nil || t.status != TransactionPendingReference || nextAttemptAt.IsZero() {
		return ErrInvalidTransactionState
	}
	t.attemptCount++
	next := nextAttemptAt.UTC()
	t.nextAttemptAt = &next
	t.updatedAt = time.Now().UTC()
	return nil
}

// RecordReferenceAttempt conta uma tentativa que terminou em falha permanente
// de infraestrutura ou expirou antes de conseguir resolver a referência.
func (t *WagerTransaction) RecordReferenceAttempt() error {
	if t == nil || t.status != TransactionPendingReference || t.attemptCount == int(^uint(0)>>1) {
		return ErrInvalidTransactionState
	}
	t.attemptCount++
	t.updatedAt = time.Now().UTC()
	return nil
}

// ScheduleInfrastructureRetry registra uma falha técnica transitória ao tentar
// resolver uma referência e agenda uma nova execução.
func (t *WagerTransaction) ScheduleInfrastructureRetry(nextAttemptAt time.Time) error {
	if t == nil || t.status != TransactionPendingReference || nextAttemptAt.IsZero() || t.attemptCount == int(^uint(0)>>1) {
		return ErrInvalidTransactionState
	}
	t.attemptCount++
	next := nextAttemptAt.UTC()
	t.nextAttemptAt = &next
	t.updatedAt = time.Now().UTC()
	return nil
}

// ResumeAfterReference retoma uma operação cuja referência foi resolvida.
func (t *WagerTransaction) ResumeAfterReference(reference WagerTransaction) error {
	if err := t.ensureNonTerminal(); err != nil {
		return err
	}
	if t.status != TransactionPendingReference {
		return ErrInvalidTransactionState
	}
	if err := t.validateReference(reference); err != nil {
		return err
	}
	t.referenceTransactionID = reference.id
	t.status = TransactionPending
	t.updatedAt = time.Now().UTC()
	return nil
}

// MarkProcessed conclui com saldo final; os estados terminais não podem ser alterados.
func (t *WagerTransaction) MarkProcessed(resultBalance Money) error {
	if err := t.ensureNonTerminal(); err != nil {
		return err
	}
	if t.status != TransactionPending {
		return ErrInvalidTransactionState
	}
	if t.referenceExternalTransactionID != "" && t.referenceTransactionID == "" {
		return ErrInvalidTransactionState
	}
	if resultBalance.Currency() != t.currency {
		return ErrTransactionCurrency
	}
	if resultBalance.Units() < 0 {
		return ErrNegativeBalance
	}
	now := time.Now().UTC()
	result := resultBalance
	t.resultBalance = &result
	t.status = TransactionProcessed
	t.updatedAt = now
	t.processedAt = &now
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
	if (t.status != TransactionPending && t.status != TransactionPendingReference) || strings.TrimSpace(failureCode) == "" {
		return ErrInvalidTransactionState
	}
	now := time.Now().UTC()
	t.status = status
	t.failureCode = failureCode
	t.updatedAt = now
	t.processedAt = &now
	return nil
}

func (t *WagerTransaction) validateReference(reference WagerTransaction) error {
	if reference.id == "" || reference.status != TransactionProcessed || reference.origin != TransactionExternal ||
		reference.providerID != t.providerID || reference.playerID != t.playerID ||
		reference.walletID != t.walletID || reference.currency != t.currency ||
		reference.roundID != t.roundID || reference.externalTransactionID != t.referenceExternalTransactionID {
		return ErrInvalidReference
	}
	if (t.kind == TransactionWin && reference.kind != TransactionBet) ||
		(t.kind == TransactionRefund && reference.kind != TransactionBet) ||
		(t.kind == TransactionRollback && reference.kind != TransactionBet && reference.kind != TransactionWin && reference.kind != TransactionRefund) {
		return ErrInvalidReference
	}
	if (t.kind == TransactionRefund || t.kind == TransactionRollback) && t.amount.Units() != reference.amount.Units() {
		return ErrInvalidReference
	}
	return nil
}

func (t *WagerTransaction) ensureNonTerminal() error {
	if t == nil {
		return ErrInvalidTransactionState
	}
	if t.status == TransactionProcessed || t.status == TransactionRejected || t.status == TransactionFailed {
		return ErrTerminalTransaction
	}
	if t.status != TransactionPending && t.status != TransactionPendingReference {
		return ErrInvalidTransactionState
	}
	return nil
}

func validateTransactionAmount(kind TransactionKind, amount Money, walletCurrency string) error {
	if amount.Currency() != walletCurrency {
		return ErrTransactionCurrency
	}
	if kind == TransactionLoss {
		if amount.Units() != 0 {
			return ErrInvalidTransactionAmount
		}
		return nil
	}
	if amount.Units() <= 0 {
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
