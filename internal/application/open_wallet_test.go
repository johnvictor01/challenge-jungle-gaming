package application

import (
	"context"
	"errors"
	"testing"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

func openWalletService(state *memoryState) (*OpenWalletService, *memoryUnitOfWork) {
	uow := &memoryUnitOfWork{state: state}
	return NewOpenWalletService(uow, &sequenceIDGenerator{}), uow
}

func TestOpenWalletWithInitialBalance(t *testing.T) {
	service, uow := openWalletService(newMemoryState())
	result, err := service.Execute(context.Background(), OpenWalletCommand{PlayerID: "player-1", InitialBalance: testMoney(2_500, "BRL")})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Wallet == nil || result.Wallet.Balance().Units() != 2_500 {
		t.Fatalf("wallet = %+v", result.Wallet)
	}
	if result.Opening == nil || result.Opening.Status() != domain.TransactionProcessed {
		t.Fatalf("opening = %+v", result.Opening)
	}
	if len(uow.state.wallets) != 1 || len(uow.state.transactions) != 1 || len(uow.state.ledger) != 1 || len(uow.state.events) != 2 {
		t.Errorf("records = wallets:%d transactions:%d ledger:%d events:%d, want 1/1/1/2", len(uow.state.wallets), len(uow.state.transactions), len(uow.state.ledger), len(uow.state.events))
	}
	if got := uow.state.ledger[0]; got.Direction() != domain.DirectionCredit || got.BalanceBefore().Units() != 0 || got.BalanceAfter().Units() != 2_500 {
		t.Errorf("opening ledger entry = %+v", got)
	}
}

func TestOpenWalletWithZeroBalance(t *testing.T) {
	service, uow := openWalletService(newMemoryState())
	result, err := service.Execute(context.Background(), OpenWalletCommand{PlayerID: "player-1", InitialBalance: testMoney(0, "BRL")})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Wallet == nil || result.Wallet.Balance().Units() != 0 || result.Opening != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(uow.state.wallets) != 1 || len(uow.state.transactions) != 0 || len(uow.state.ledger) != 0 || len(uow.state.events) != 0 {
		t.Errorf("zero-balance opening created extra records")
	}
}

func TestOpenWalletRejectsDuplicatePlayerCurrency(t *testing.T) {
	state := newMemoryState()
	seedMemoryWallet(t, state, 0)
	service, _ := openWalletService(state)
	_, err := service.Execute(context.Background(), OpenWalletCommand{PlayerID: "player-1", InitialBalance: testMoney(0, "BRL")})
	if !errors.Is(err, ErrWalletAlreadyExists) {
		t.Fatalf("error = %v, want ErrWalletAlreadyExists", err)
	}
}

func TestOpenWalletRejectsInvalidInput(t *testing.T) {
	for _, command := range []OpenWalletCommand{
		{InitialBalance: testMoney(0, "BRL")},
		{PlayerID: "player-1", InitialBalance: testMoney(-1, "BRL")},
		{PlayerID: "player-1", InitialBalance: domain.Money{}},
	} {
		service, _ := openWalletService(newMemoryState())
		if _, err := service.Execute(context.Background(), command); err == nil {
			t.Errorf("Execute(%+v) error = nil, want validation error", command)
		}
	}
}

func TestOpenWalletPersistenceFailure(t *testing.T) {
	state := newMemoryState()
	state.failOn = "outbox_append"
	service, uow := openWalletService(state)
	_, err := service.Execute(context.Background(), OpenWalletCommand{PlayerID: "player-1", InitialBalance: testMoney(2_500, "BRL")})
	if err == nil {
		t.Fatal("Execute() error = nil, want injected failure")
	}
	if len(uow.state.wallets) != 0 || len(uow.state.transactions) != 0 || len(uow.state.ledger) != 0 || len(uow.state.events) != 0 {
		t.Errorf("UnitOfWork did not roll back wallet opening")
	}
}
