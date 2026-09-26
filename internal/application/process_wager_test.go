package application

import (
	"context"
	"errors"
	"testing"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

func processService(state *memoryState) (*ProcessWagerService, *memoryUnitOfWork) {
	uow := &memoryUnitOfWork{state: state}
	return NewProcessWagerService(uow, &sequenceIDGenerator{}), uow
}

func wagerCommand(wallet *domain.Wallet, kind domain.TransactionKind, amount int64, key, externalID string) ProcessWagerCommand {
	return ProcessWagerCommand{
		WalletID: wallet.ID, PlayerID: wallet.PlayerID, ProviderID: "provider-1",
		ExternalTransactionID: externalID, IdempotencyKey: key, RoundID: "round-1", GameID: "game-1",
		Kind: kind, Amount: domain.Money{Units: amount, Currency: wallet.Currency},
	}
}

func TestProcessWagerBetSuccess(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 10_000)
	service, uow := processService(state)

	result, err := service.Execute(context.Background(), wagerCommand(wallet, domain.TransactionBet, 2_500, "idem-1", "external-1"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionProcessed || result.Balance == nil || result.Balance.Units != 7_500 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := uow.state.wallets[wallet.ID].Balance.Units; got != 7_500 {
		t.Errorf("balance = %d, want 7500", got)
	}
	if len(uow.state.ledger) != 1 || uow.state.ledger[0].Direction != domain.DirectionDebit {
		t.Errorf("ledger = %+v, want one debit", uow.state.ledger)
	}
	if len(uow.state.events) != 2 {
		t.Errorf("events = %d, want 2", len(uow.state.events))
	}
}

func TestProcessWagerInsufficientFunds(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 1_000)
	service, uow := processService(state)
	result, err := service.Execute(context.Background(), wagerCommand(wallet, domain.TransactionBet, 2_000, "idem-1", "external-1"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionRejected || result.FailureCode != "INSUFFICIENT_FUNDS" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := uow.state.wallets[wallet.ID].Balance.Units; got != 1_000 {
		t.Errorf("balance = %d, want 1000", got)
	}
	if len(uow.state.ledger) != 0 || len(uow.state.events) != 1 {
		t.Errorf("ledger/events = %d/%d, want 0/1", len(uow.state.ledger), len(uow.state.events))
	}
}

func TestProcessWagerLossDoesNotChangeWallet(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 1_000)
	service, uow := processService(state)
	result, err := service.Execute(context.Background(), wagerCommand(wallet, domain.TransactionLoss, 0, "idem-1", "external-1"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionProcessed || result.Balance == nil || result.Balance.Units != 1_000 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := uow.state.wallets[wallet.ID].Version; got != 1 {
		t.Errorf("wallet version = %d, want 1", got)
	}
	if len(uow.state.ledger) != 0 || len(uow.state.events) != 1 {
		t.Errorf("ledger/events = %d/%d, want 0/1", len(uow.state.ledger), len(uow.state.events))
	}
}

func TestProcessWagerIdempotentReplay(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 10_000)
	service, uow := processService(state)
	command := wagerCommand(wallet, domain.TransactionBet, 2_500, "idem-1", "external-1")
	first, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if first.TransactionID != second.TransactionID || !second.IdempotentReplay {
		t.Errorf("first/second = %+v / %+v", first, second)
	}
	if len(uow.state.transactions) != 1 || len(uow.state.ledger) != 1 || len(uow.state.events) != 2 {
		t.Errorf("replay duplicated records: tx=%d ledger=%d events=%d", len(uow.state.transactions), len(uow.state.ledger), len(uow.state.events))
	}
}

func TestProcessWagerIdempotencyConflict(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 10_000)
	service, _ := processService(state)
	_, err := service.Execute(context.Background(), wagerCommand(wallet, domain.TransactionBet, 2_500, "idem-1", "external-1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(context.Background(), wagerCommand(wallet, domain.TransactionBet, 3_000, "idem-1", "external-2"))
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestProcessWagerExternalIDConflict(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 10_000)
	service, _ := processService(state)
	_, err := service.Execute(context.Background(), wagerCommand(wallet, domain.TransactionBet, 2_500, "idem-1", "external-1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(context.Background(), wagerCommand(wallet, domain.TransactionBet, 2_500, "idem-2", "external-1"))
	if !errors.Is(err, ErrExternalTransactionConflict) {
		t.Fatalf("error = %v, want ErrExternalTransactionConflict", err)
	}
}

func TestProcessWagerMissingReference(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 500)
	service, uow := processService(state)
	command := wagerCommand(wallet, domain.TransactionRefund, 1_500, "idem-1", "refund-1")
	command.ReferenceExternalTransactionID = "bet-not-yet-arrived"
	result, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionPendingReference {
		t.Fatalf("status = %s, want PENDING_REFERENCE", result.Status)
	}
	if uow.state.wallets[wallet.ID].Balance.Units != 500 || len(uow.state.ledger) != 0 || len(uow.state.events) != 1 {
		t.Errorf("missing reference changed wallet or emitted wrong records")
	}
}

func TestProcessWagerResolvedReference(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 500)
	reference := &domain.WagerTransaction{ID: "bet-id", Origin: domain.TransactionExternal, WalletID: wallet.ID, PlayerID: wallet.PlayerID, Currency: wallet.Currency, ProviderID: "provider-1", ExternalTransactionID: "bet-1", Kind: domain.TransactionBet, Amount: domain.Money{Units: 1_500, Currency: "BRL"}, RoundID: "round-1", Status: domain.TransactionProcessed}
	state.transactions[reference.ID] = reference
	service, uow := processService(state)
	command := wagerCommand(wallet, domain.TransactionRefund, 1_500, "idem-1", "refund-1")
	command.ReferenceExternalTransactionID = "bet-1"
	result, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionProcessed || result.Balance == nil || result.Balance.Units != 2_000 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(uow.state.ledger) != 1 || uow.state.ledger[0].Direction != domain.DirectionCredit {
		t.Errorf("ledger = %+v, want one credit", uow.state.ledger)
	}
}

func TestProcessWagerRejectsSecondReversalOfReference(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 3_000)
	reference := &domain.WagerTransaction{ID: "bet-id", Origin: domain.TransactionExternal, WalletID: wallet.ID, PlayerID: wallet.PlayerID, Currency: wallet.Currency, ProviderID: "provider-1", ExternalTransactionID: "bet-1", Kind: domain.TransactionBet, Amount: domain.Money{Units: 1_500, Currency: "BRL"}, RoundID: "round-1", Status: domain.TransactionProcessed}
	previousRefund := &domain.WagerTransaction{ID: "refund-id", Origin: domain.TransactionExternal, WalletID: wallet.ID, PlayerID: wallet.PlayerID, Currency: wallet.Currency, ProviderID: "provider-1", ExternalTransactionID: "refund-1", IdempotencyKey: "refund-key", Kind: domain.TransactionRefund, ReferenceExternalTransactionID: "bet-1", ReferenceTransactionID: reference.ID, Amount: domain.Money{Units: 1_500, Currency: "BRL"}, RoundID: "round-1", Status: domain.TransactionProcessed}
	state.transactions[reference.ID] = reference
	state.transactions[previousRefund.ID] = previousRefund
	service, uow := processService(state)
	command := wagerCommand(wallet, domain.TransactionRollback, 1_500, "rollback-key", "rollback-1")
	command.ReferenceExternalTransactionID = "bet-1"
	result, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionRejected || result.FailureCode != "REFERENCE_ALREADY_REVERSED" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if uow.state.wallets[wallet.ID].Balance.Units != 3_000 || len(uow.state.ledger) != 0 {
		t.Errorf("duplicate reversal changed wallet or ledger")
	}
}

func TestProcessWagerRollbackInsufficientBalance(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 500)
	reference := &domain.WagerTransaction{ID: "win-id", Origin: domain.TransactionExternal, WalletID: wallet.ID, PlayerID: wallet.PlayerID, Currency: wallet.Currency, ProviderID: "provider-1", ExternalTransactionID: "win-1", Kind: domain.TransactionWin, Amount: domain.Money{Units: 1_500, Currency: "BRL"}, RoundID: "round-1", Status: domain.TransactionProcessed}
	state.transactions[reference.ID] = reference
	service, uow := processService(state)
	command := wagerCommand(wallet, domain.TransactionRollback, 1_500, "idem-1", "rollback-1")
	command.ReferenceExternalTransactionID = "win-1"
	result, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != domain.TransactionRejected || result.FailureCode != "REVERSAL_INSUFFICIENT_FUNDS" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if uow.state.wallets[wallet.ID].Balance.Units != 500 || len(uow.state.ledger) != 0 {
		t.Errorf("rejected rollback changed wallet or ledger")
	}
}

func TestProcessWagerPersistenceFailure(t *testing.T) {
	state := newMemoryState()
	wallet := seedMemoryWallet(t, state, 10_000)
	state.failOn = "outbox_append"
	service, uow := processService(state)
	_, err := service.Execute(context.Background(), wagerCommand(wallet, domain.TransactionBet, 2_500, "idem-1", "external-1"))
	if err == nil {
		t.Fatal("Execute() error = nil, want injected persistence failure")
	}
	if uow.state.wallets[wallet.ID].Balance.Units != 10_000 || len(uow.state.transactions) != 0 || len(uow.state.ledger) != 0 || len(uow.state.events) != 0 {
		t.Errorf("UnitOfWork did not roll back all changes: %+v", uow.state)
	}
}
