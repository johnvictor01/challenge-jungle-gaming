package application

import (
	"context"
	"errors"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

var (
	ErrNotFound                    = errors.New("record not found")
	ErrWalletAlreadyExists         = errors.New("wallet already exists for player and currency")
	ErrIdempotencyConflict         = errors.New("idempotency key was used with a different payload")
	ErrExternalTransactionConflict = errors.New("external transaction ID was used with another idempotency key")
	ErrPersistenceConflict         = errors.New("persistence uniqueness conflict")
	ErrPayloadHashMismatch         = errors.New("provided payload hash does not match normalized business fields")
)

// IDGenerator fornece IDs para registros que ainda não têm identificador.
type IDGenerator interface {
	NewID() (string, error)
}

// UnitOfWork executa todas as operações do callback em uma única transação.
// O adapter PostgreSQL deve fazer rollback se o callback ou o commit falhar.
type UnitOfWork interface {
	WithinTransaction(ctx context.Context, callback func(Repositories) error) error
}

// InboxUnitOfWork commits delivery receipt and domain changes atomically.
type InboxUnitOfWork interface {
	WithinInboxTransaction(ctx context.Context, consumerName, messageID, payloadHash string, callback func(Repositories, bool) error) error
}

// Repositories contém os repositórios ligados à mesma transação SQL.
type Repositories struct {
	Wallets      WalletRepository
	Transactions WagerTransactionRepository
	Ledger       WalletLedgerRepository
	Outbox       OutboxRepository
	Inbox        InboxRepository
}

type InboxRepository interface {
	Find(ctx context.Context, consumerName, messageID string) (InboxMessage, error)
	Complete(ctx context.Context, consumerName, messageID, transactionID string, completedAt time.Time) error
}

type InboxMessage struct {
	ConsumerName  string
	MessageID     string
	PayloadHash   string
	ReceivedAt    time.Time
	CompletedAt   *time.Time
	TransactionID string
}

// WalletRepository lê e grava carteiras. FindByID deve bloquear a linha durante o UnitOfWork.
type WalletRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Wallet, error)
	FindByPlayerAndCurrency(ctx context.Context, playerID, currency string) (*domain.Wallet, error)
	Create(ctx context.Context, wallet *domain.Wallet) error
	Update(ctx context.Context, wallet *domain.Wallet, expectedVersion int64) error
}

// WagerTransactionRepository persiste operações e procura seus dados de replay/referência.
type WagerTransactionRepository interface {
	FindByID(ctx context.Context, id string) (*domain.WagerTransaction, error)
	FindByIDForUpdate(ctx context.Context, id string) (*domain.WagerTransaction, error)
	FindByIdempotencyKey(ctx context.Context, providerID, key string) (*domain.WagerTransaction, error)
	FindByExternalID(ctx context.Context, providerID, externalID string) (*domain.WagerTransaction, error)
	FindSuccessfulReversal(ctx context.Context, referenceTransactionID string) (*domain.WagerTransaction, error)
	Create(ctx context.Context, transaction *domain.WagerTransaction) error
	Update(ctx context.Context, transaction *domain.WagerTransaction) error
}

// WalletLedgerRepository só insere lançamentos; não deve oferecer edição ou exclusão.
type WalletLedgerRepository interface {
	Create(ctx context.Context, entry *domain.WalletLedgerEntry) error
	ListByWallet(ctx context.Context, walletID string, afterCreatedAt *time.Time, afterID string, limit int) ([]*domain.WalletLedgerEntry, error)
	SummarizeByWallet(ctx context.Context, walletID string) (LedgerSummary, error)
}

type LedgerSummary struct {
	Credits int64
	Debits  int64
	Count   int64
}

// OutboxRepository persiste eventos dentro da mesma transação SQL do agregado.
type OutboxRepository interface {
	Append(ctx context.Context, event OutboxEvent) error
}

// OutboxDispatchRepository administra leases para publicar eventos já commitados.
// Claim precisa ser atômico entre instâncias e ignorar eventos com lease válida.
type OutboxDispatchRepository interface {
	Claim(ctx context.Context, owner string, limit int, lease time.Duration) ([]OutboxEvent, error)
	MarkPublished(ctx context.Context, eventID, owner string, publishedAt time.Time) error
	ScheduleRetry(ctx context.Context, eventID, owner string, nextAttemptAt time.Time, lastError string) error
	MarkFailed(ctx context.Context, eventID, owner, lastError string) error
}
