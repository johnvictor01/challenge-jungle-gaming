package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

func integrationPlayerID(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("integration-%s-%d", t.Name(), time.Now().UnixNano())
}

func integrationStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL after applying migrations to run PostgreSQL integration tests")
	}
	store, err := NewPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestPostgresStoreProcessesWagerAtomically(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	operationID := integrationPlayerID(t)
	open := application.NewOpenWalletService(store, ids)
	opened, err := open.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID:       integrationPlayerID(t),
		InitialBalance: domain.Money{Units: 10_000, Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	process := application.NewProcessWagerService(store, ids)
	command := application.ProcessWagerCommand{
		WalletID: opened.Wallet.ID, PlayerID: opened.Wallet.PlayerID, ProviderID: "integration-provider-" + operationID,
		ExternalTransactionID: "integration-bet-" + operationID, IdempotencyKey: "integration-idem-" + operationID,
		RoundID: "round-1", GameID: "game-1", Kind: domain.TransactionBet,
		Amount: domain.Money{Units: 2_500, Currency: "BRL"},
	}
	result, err := process.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("process wager: %v", err)
	}
	if result.Status != domain.TransactionProcessed || result.Balance == nil || result.Balance.Units != 7_500 {
		t.Fatalf("unexpected result: %+v", result)
	}
	replay, err := process.Execute(context.Background(), command)
	if err != nil || !replay.IdempotentReplay || replay.Balance == nil || replay.Balance.Units != 7_500 {
		t.Fatalf("unexpected persisted replay: result=%+v error=%v", replay, err)
	}

	var balance int64
	var version int64
	if err := store.pool.QueryRow(context.Background(), "SELECT balance_minor, version FROM wallets WHERE id = $1", opened.Wallet.ID).Scan(&balance, &version); err != nil {
		t.Fatal(err)
	}
	if balance != 7_500 || version != 2 {
		t.Errorf("wallet = balance:%d version:%d, want 7500/2", balance, version)
	}
	var ledgerCount, eventCount int
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id = $1", opened.Wallet.ID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox_events WHERE aggregate_id = $1", opened.Wallet.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 || eventCount != 4 {
		t.Errorf("ledger/events = %d/%d, want 2/4", ledgerCount, eventCount)
	}
	queries := application.NewQueryService(store)
	reconciliation, err := queries.Reconcile(context.Background(), opened.Wallet.ID)
	if err != nil || !reconciliation.Consistent || reconciliation.CheckedEntries != 2 {
		t.Errorf("reconciliation = %+v, error=%v", reconciliation, err)
	}
	firstPage, err := queries.Ledger(context.Background(), opened.Wallet.ID, "", 1)
	if err != nil || len(firstPage.Entries) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("first ledger page=%+v error=%v", firstPage, err)
	}
	secondPage, err := queries.Ledger(context.Background(), opened.Wallet.ID, firstPage.NextCursor, 1)
	if err != nil || len(secondPage.Entries) != 1 || firstPage.Entries[0].ID == secondPage.Entries[0].ID {
		t.Fatalf("second ledger page=%+v error=%v", secondPage, err)
	}
}

func TestPostgresLedgerIsAppendOnly(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{
		PlayerID:       integrationPlayerID(t),
		InitialBalance: domain.Money{Units: 1_000, Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	_, err = store.pool.Exec(context.Background(), "DELETE FROM wallet_ledger_entries WHERE wallet_id = $1", opened.Wallet.ID)
	if err == nil {
		t.Fatal("DELETE ledger entry succeeded, want append-only trigger error")
	}
}

func TestPostgresEnforcesWalletUniquenessAndProviderIsolation(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	playerID := integrationPlayerID(t)
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: domain.Money{Units: 1_000, Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	_, err = application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: domain.Money{Currency: "BRL"},
	})
	if err != application.ErrWalletAlreadyExists {
		t.Fatalf("duplicate wallet error = %v, want ErrWalletAlreadyExists", err)
	}
	process := application.NewProcessWagerService(store, ids)
	_, err = process.Execute(context.Background(), application.ProcessWagerCommand{
		WalletID: opened.Wallet.ID, PlayerID: opened.Wallet.PlayerID, ProviderID: "provider-owned-" + playerID,
		ExternalTransactionID: "provider-tx-" + playerID, IdempotencyKey: "provider-key-" + playerID,
		RoundID: "round-1", GameID: "game-1", Kind: domain.TransactionLoss,
		Amount: domain.Money{Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("create provider transaction: %v", err)
	}
	queries := application.NewQueryService(store)
	if _, err := queries.ProviderTransaction(context.Background(), "another-provider", "provider-tx-"+playerID); err != application.ErrNotFound {
		t.Errorf("cross-provider query error = %v, want ErrNotFound", err)
	}
}

func TestPostgresStoreSerializesConcurrentDebits(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	operationID := integrationPlayerID(t)
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{
		PlayerID:       integrationPlayerID(t),
		InitialBalance: domain.Money{Units: 10_000, Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	service := application.NewProcessWagerService(store, ids)
	commands := []application.ProcessWagerCommand{
		{WalletID: opened.Wallet.ID, PlayerID: opened.Wallet.PlayerID, ProviderID: "concurrent-provider-" + operationID, ExternalTransactionID: "concurrent-bet-a-" + operationID, IdempotencyKey: "concurrent-key-a-" + operationID, RoundID: "round-1", GameID: "game-1", Kind: domain.TransactionBet, Amount: domain.Money{Units: 8_000, Currency: "BRL"}},
		{WalletID: opened.Wallet.ID, PlayerID: opened.Wallet.PlayerID, ProviderID: "concurrent-provider-" + operationID, ExternalTransactionID: "concurrent-bet-b-" + operationID, IdempotencyKey: "concurrent-key-b-" + operationID, RoundID: "round-1", GameID: "game-1", Kind: domain.TransactionBet, Amount: domain.Money{Units: 8_000, Currency: "BRL"}},
	}
	results := make(chan application.ProcessWagerResult, len(commands))
	errors := make(chan error, len(commands))
	var group sync.WaitGroup
	for _, command := range commands {
		group.Add(1)
		go func(command application.ProcessWagerCommand) {
			defer group.Done()
			result, err := service.Execute(context.Background(), command)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(command)
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Errorf("concurrent Execute() error = %v", err)
	}
	var processed, rejected int
	for result := range results {
		switch result.Status {
		case domain.TransactionProcessed:
			processed++
		case domain.TransactionRejected:
			if result.FailureCode != "INSUFFICIENT_FUNDS" {
				t.Errorf("rejection code = %s, want INSUFFICIENT_FUNDS", result.FailureCode)
			}
			rejected++
		default:
			t.Errorf("unexpected status %s", result.Status)
		}
	}
	if processed != 1 || rejected != 1 {
		t.Fatalf("processed/rejected = %d/%d, want 1/1", processed, rejected)
	}
	var balance int64
	if err := store.pool.QueryRow(context.Background(), "SELECT balance_minor FROM wallets WHERE id = $1", opened.Wallet.ID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 2_000 {
		t.Errorf("balance = %d, want 2000", balance)
	}
}
