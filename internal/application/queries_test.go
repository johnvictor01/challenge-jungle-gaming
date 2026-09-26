package application

import (
	"context"
	"testing"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

func TestQueryServiceReconcilesBalanceAndPaginatesLedger(t *testing.T) {
	state := newMemoryState()
	uow := &memoryUnitOfWork{state: state}
	ids := &sequenceIDGenerator{}
	opened, err := NewOpenWalletService(uow, ids).Execute(context.Background(), OpenWalletCommand{
		PlayerID: "player-query", InitialBalance: domain.Money{Units: 10_000, Currency: "BRL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewProcessWagerService(uow, ids)
	for _, command := range []ProcessWagerCommand{
		{WalletID: opened.Wallet.ID, PlayerID: opened.Wallet.PlayerID, ProviderID: "provider", ExternalTransactionID: "bet", IdempotencyKey: "idem-bet", RoundID: "round", GameID: "game", Kind: domain.TransactionBet, Amount: domain.Money{Units: 2_500, Currency: "BRL"}},
		{WalletID: opened.Wallet.ID, PlayerID: opened.Wallet.PlayerID, ProviderID: "provider", ExternalTransactionID: "win", IdempotencyKey: "idem-win", RoundID: "round", GameID: "game", Kind: domain.TransactionWin, Amount: domain.Money{Units: 1_000, Currency: "BRL"}},
	} {
		if _, err := service.Execute(context.Background(), command); err != nil {
			t.Fatal(err)
		}
	}
	queries := NewQueryService(uow)
	check, err := queries.Reconcile(context.Background(), opened.Wallet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !check.Consistent || check.StoredBalance.Units != 8_500 || check.CalculatedBalance.Units != 8_500 || check.CheckedEntries != 3 {
		t.Fatalf("reconciliation = %+v", check)
	}
	first, err := queries.Ledger(context.Background(), opened.Wallet.ID, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := queries.Ledger(context.Background(), opened.Wallet.ID, first.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 1 || second.Entries[0].ID == first.Entries[0].ID {
		t.Fatalf("second page = %+v", second)
	}
}

func TestQueryServiceRejectsInvalidLedgerCursorAndLimit(t *testing.T) {
	state := newMemoryState()
	queries := NewQueryService(&memoryUnitOfWork{state: state})
	if _, err := queries.Ledger(context.Background(), "missing", "not-a-cursor", 10); err != ErrInvalidCursor {
		t.Errorf("invalid cursor error = %v, want ErrInvalidCursor", err)
	}
	if _, err := queries.Ledger(context.Background(), "missing", "", 101); err == nil {
		t.Error("limit 101 should be rejected")
	}
}
