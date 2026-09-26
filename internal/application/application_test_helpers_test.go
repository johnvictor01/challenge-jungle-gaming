package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type memoryState struct {
	wallets      map[string]*domain.Wallet
	transactions map[string]*domain.WagerTransaction
	ledger       []*domain.WalletLedgerEntry
	events       []OutboxEvent
	inbox        map[string]InboxMessage
	failOn       string
}

func newMemoryState() *memoryState {
	return &memoryState{
		wallets:      map[string]*domain.Wallet{},
		transactions: map[string]*domain.WagerTransaction{},
		inbox:        map[string]InboxMessage{},
	}
}

func cloneMemoryState(source *memoryState) *memoryState {
	copy := newMemoryState()
	copy.failOn = source.failOn
	for key, message := range source.inbox {
		copy.inbox[key] = message
	}
	for id, wallet := range source.wallets {
		walletCopy := *wallet
		copy.wallets[id] = &walletCopy
	}
	for id, transaction := range source.transactions {
		copy.transactions[id] = cloneTransaction(transaction)
	}
	for _, entry := range source.ledger {
		entryCopy := *entry
		copy.ledger = append(copy.ledger, &entryCopy)
	}
	for _, event := range source.events {
		event.Data = append([]byte(nil), event.Data...)
		copy.events = append(copy.events, event)
	}
	return copy
}

func cloneTransaction(transaction *domain.WagerTransaction) *domain.WagerTransaction {
	return transaction.Clone()
}

func rehydratedTransaction(state domain.WagerTransactionState) *domain.WagerTransaction {
	transaction, err := domain.RehydrateWagerTransaction(state)
	if err != nil {
		panic(err)
	}
	return transaction
}

func processedExternalTransaction(wallet *domain.Wallet, id, externalID string, kind domain.TransactionKind, amount int64, reference string) *domain.WagerTransaction {
	tx, err := domain.NewExternalWagerTransaction(domain.ExternalWagerInput{
		ID: id, Wallet: wallet, ProviderID: "provider-1", ExternalTransactionID: externalID, IdempotencyKey: "idem-" + id,
		PayloadHash: strings.Repeat("a", 64), Kind: kind, Amount: testMoney(amount, wallet.Currency()), RoundID: "round-1", GameID: "game-1",
		ReferenceExternalTransactionID: reference,
	})
	if err != nil {
		panic(err)
	}
	if kind == domain.TransactionRefund || kind == domain.TransactionRollback || (kind == domain.TransactionWin && reference != "") {
		// A processed fixture with a reference is rehydrated with its resolved IDs below.
		state := tx.State()
		state.ReferenceTransactionID = "bet-id"
		state.Status = domain.TransactionProcessed
		now := time.Now().UTC()
		state.ProcessedAt = &now
		state.ResultBalance = moneyPointer(wallet.Balance())
		processed, err := domain.RehydrateWagerTransaction(state)
		if err != nil {
			panic(err)
		}
		return processed
	}
	if err := tx.MarkProcessed(wallet.Balance()); err != nil {
		panic(err)
	}
	return tx
}

func moneyPointer(value domain.Money) *domain.Money { copy := value; return &copy }

type memoryUnitOfWork struct {
	state *memoryState
}

func testMoney(units int64, currency string) domain.Money {
	money, err := domain.NewMoney(units, currency)
	if err != nil {
		panic(err)
	}
	return money
}

func (u *memoryUnitOfWork) WithinTransaction(ctx context.Context, callback func(Repositories) error) error {
	working := cloneMemoryState(u.state)
	repositories := Repositories{
		Wallets:      memoryWalletRepository{state: working},
		Transactions: memoryTransactionRepository{state: working},
		Ledger:       memoryLedgerRepository{state: working},
		Outbox:       memoryOutboxRepository{state: working},
		Inbox:        memoryInboxRepository{state: working},
	}
	if err := callback(repositories); err != nil {
		return err
	}
	u.state = working
	return nil
}

func (u *memoryUnitOfWork) WithinInboxTransaction(_ context.Context, consumerName, messageID, payloadHash string, callback func(Repositories, bool) error) error {
	working := cloneMemoryState(u.state)
	key := consumerName + ":" + messageID
	_, duplicate := working.inbox[key]
	if !duplicate {
		working.inbox[key] = InboxMessage{ConsumerName: consumerName, MessageID: messageID, PayloadHash: payloadHash, ReceivedAt: time.Now()}
	}
	repositories := Repositories{
		Wallets: memoryWalletRepository{state: working}, Transactions: memoryTransactionRepository{state: working},
		Ledger: memoryLedgerRepository{state: working}, Outbox: memoryOutboxRepository{state: working},
		Inbox: memoryInboxRepository{state: working},
	}
	if err := callback(repositories, duplicate); err != nil {
		return err
	}
	u.state = working
	return nil
}

type memoryWalletRepository struct{ state *memoryState }

func (r memoryWalletRepository) FindByID(_ context.Context, id string) (*domain.Wallet, error) {
	wallet := r.state.wallets[id]
	if wallet == nil {
		return nil, ErrNotFound
	}
	copy := *wallet
	return &copy, nil
}

func (r memoryWalletRepository) FindByPlayerAndCurrency(_ context.Context, playerID, currency string) (*domain.Wallet, error) {
	for _, wallet := range r.state.wallets {
		if wallet.PlayerID() == playerID && wallet.Currency() == currency {
			copy := *wallet
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}

func (r memoryWalletRepository) Create(_ context.Context, wallet *domain.Wallet) error {
	if r.state.failOn == "wallet_create" {
		return errors.New("injected wallet create failure")
	}
	for _, existing := range r.state.wallets {
		if existing.PlayerID() == wallet.PlayerID() && existing.Currency() == wallet.Currency() {
			return ErrPersistenceConflict
		}
	}
	copy := *wallet
	r.state.wallets[wallet.ID()] = &copy
	return nil
}

func (r memoryWalletRepository) Update(_ context.Context, wallet *domain.Wallet, expectedVersion int64) error {
	if r.state.failOn == "wallet_update" {
		return errors.New("injected wallet update failure")
	}
	current := r.state.wallets[wallet.ID()]
	if current == nil {
		return ErrNotFound
	}
	if current.Version() != expectedVersion {
		return ErrPersistenceConflict
	}
	copy := *wallet
	r.state.wallets[wallet.ID()] = &copy
	return nil
}

type memoryTransactionRepository struct{ state *memoryState }

func (r memoryTransactionRepository) FindByID(_ context.Context, id string) (*domain.WagerTransaction, error) {
	transaction := r.state.transactions[id]
	if transaction == nil {
		return nil, ErrNotFound
	}
	return cloneTransaction(transaction), nil
}

func (r memoryTransactionRepository) FindByIDForUpdate(ctx context.Context, id string) (*domain.WagerTransaction, error) {
	return r.FindByID(ctx, id)
}

func (r memoryTransactionRepository) FindByIdempotencyKey(_ context.Context, providerID, key string) (*domain.WagerTransaction, error) {
	for _, transaction := range r.state.transactions {
		if transaction.ProviderID() == providerID && transaction.IdempotencyKey() == key && transaction.Origin() == domain.TransactionExternal {
			return cloneTransaction(transaction), nil
		}
	}
	return nil, ErrNotFound
}

func (r memoryTransactionRepository) FindByExternalID(_ context.Context, providerID, externalID string) (*domain.WagerTransaction, error) {
	for _, transaction := range r.state.transactions {
		if transaction.ProviderID() == providerID && transaction.ExternalTransactionID() == externalID && transaction.Origin() == domain.TransactionExternal {
			return cloneTransaction(transaction), nil
		}
	}
	return nil, ErrNotFound
}

func (r memoryTransactionRepository) FindSuccessfulReversal(_ context.Context, referenceTransactionID string) (*domain.WagerTransaction, error) {
	for _, transaction := range r.state.transactions {
		if transaction.Status() == domain.TransactionProcessed && transaction.ReferenceTransactionID() == referenceTransactionID &&
			(transaction.Kind() == domain.TransactionRefund || transaction.Kind() == domain.TransactionRollback) {
			return cloneTransaction(transaction), nil
		}
	}
	return nil, ErrNotFound
}

func (r memoryTransactionRepository) Create(_ context.Context, transaction *domain.WagerTransaction) error {
	if r.state.failOn == "transaction_create" {
		return errors.New("injected transaction create failure")
	}
	if _, exists := r.state.transactions[transaction.ID()]; exists {
		return ErrPersistenceConflict
	}
	for _, existing := range r.state.transactions {
		if transaction.Origin() == domain.TransactionExternal && existing.Origin() == domain.TransactionExternal &&
			((existing.ProviderID() == transaction.ProviderID() && existing.IdempotencyKey() == transaction.IdempotencyKey()) ||
				(existing.ProviderID() == transaction.ProviderID() && existing.ExternalTransactionID() == transaction.ExternalTransactionID())) {
			return ErrPersistenceConflict
		}
		if transaction.Kind() == domain.TransactionOpening && existing.WalletID() == transaction.WalletID() && existing.Kind() == domain.TransactionOpening {
			return ErrPersistenceConflict
		}
	}
	r.state.transactions[transaction.ID()] = cloneTransaction(transaction)
	return nil
}

func (r memoryTransactionRepository) Update(_ context.Context, transaction *domain.WagerTransaction) error {
	if _, exists := r.state.transactions[transaction.ID()]; !exists {
		return ErrNotFound
	}
	r.state.transactions[transaction.ID()] = cloneTransaction(transaction)
	return nil
}

type memoryLedgerRepository struct{ state *memoryState }

func (r memoryLedgerRepository) Create(_ context.Context, entry *domain.WalletLedgerEntry) error {
	if r.state.failOn == "ledger_create" {
		return errors.New("injected ledger create failure")
	}
	copy := *entry
	r.state.ledger = append(r.state.ledger, &copy)
	return nil
}

func (r memoryLedgerRepository) ListByWallet(_ context.Context, walletID string, afterCreatedAt *time.Time, afterID string, limit int) ([]*domain.WalletLedgerEntry, error) {
	entries := make([]*domain.WalletLedgerEntry, 0)
	for _, entry := range r.state.ledger {
		if entry.WalletID() != walletID {
			continue
		}
		if afterCreatedAt != nil && !(entry.CreatedAt().After(*afterCreatedAt) || (entry.CreatedAt().Equal(*afterCreatedAt) && entry.ID() > afterID)) {
			continue
		}
		copy := *entry
		entries = append(entries, &copy)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt().Equal(entries[j].CreatedAt()) {
			return entries[i].ID() < entries[j].ID()
		}
		return entries[i].CreatedAt().Before(entries[j].CreatedAt())
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (r memoryLedgerRepository) SummarizeByWallet(_ context.Context, walletID string) (LedgerSummary, error) {
	var summary LedgerSummary
	for _, entry := range r.state.ledger {
		if entry.WalletID() != walletID {
			continue
		}
		summary.Count++
		if entry.Direction() == domain.DirectionCredit {
			if entry.Amount().Units() > int64(^uint64(0)>>1)-summary.Credits {
				return LedgerSummary{}, domain.ErrOverflow
			}
			summary.Credits += entry.Amount().Units()
		} else {
			if entry.Amount().Units() > int64(^uint64(0)>>1)-summary.Debits {
				return LedgerSummary{}, domain.ErrOverflow
			}
			summary.Debits += entry.Amount().Units()
		}
	}
	return summary, nil
}

type memoryInboxRepository struct{ state *memoryState }

func (r memoryInboxRepository) Find(_ context.Context, consumerName, messageID string) (InboxMessage, error) {
	message, ok := r.state.inbox[consumerName+":"+messageID]
	if !ok {
		return InboxMessage{}, ErrNotFound
	}
	return message, nil
}
func (r memoryInboxRepository) Create(_ context.Context, message InboxMessage) error {
	key := message.ConsumerName + ":" + message.MessageID
	if _, exists := r.state.inbox[key]; exists {
		return ErrPersistenceConflict
	}
	r.state.inbox[key] = message
	return nil
}
func (r memoryInboxRepository) Complete(_ context.Context, consumerName, messageID, transactionID string, completedAt time.Time) error {
	if r.state.failOn == "inbox_complete" {
		return errors.New("injected inbox completion failure")
	}
	key := consumerName + ":" + messageID
	message, ok := r.state.inbox[key]
	if !ok {
		return ErrNotFound
	}
	message.CompletedAt = &completedAt
	message.TransactionID = transactionID
	r.state.inbox[key] = message
	return nil
}

type memoryOutboxRepository struct{ state *memoryState }

func (r memoryOutboxRepository) Append(_ context.Context, event OutboxEvent) error {
	if r.state.failOn == "outbox_append" {
		return errors.New("injected outbox append failure")
	}
	event.Data = append([]byte(nil), event.Data...)
	r.state.events = append(r.state.events, event)
	return nil
}

type sequenceIDGenerator struct{ next int }

func (g *sequenceIDGenerator) NewID() (string, error) {
	g.next++
	return fmt.Sprintf("id-%03d", g.next), nil
}

func seedMemoryWallet(t testFataler, state *memoryState, balance int64) *domain.Wallet {
	t.Helper()
	wallet, err := domain.NewWallet("player-1", "BRL", balance)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	state.wallets[wallet.ID()] = wallet
	return wallet
}

type testFataler interface {
	Helper()
	Fatalf(format string, args ...any)
}
