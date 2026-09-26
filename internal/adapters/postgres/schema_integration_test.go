package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	commandA := integrationBetCommand(opened.Wallet, operationID, "a", 8_000)
	commandB := integrationBetCommand(opened.Wallet, operationID, "b", 8_000)
	results := runPostgresWorkerProcesses(t, os.Getenv("TEST_DATABASE_URL"), [][]application.ProcessWagerCommand{{commandA}, {commandB}, {commandA}})
	var processed, rejected int
	for _, result := range results {
		switch domain.TransactionStatus(result.Status) {
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
	if processed+rejected != 3 || rejected != 1 {
		t.Fatalf("results processed/rejected = %d/%d, want three results containing exactly one rejection", processed, rejected)
	}
	var balance int64
	if err := store.pool.QueryRow(context.Background(), "SELECT balance_minor FROM wallets WHERE id = $1", opened.Wallet.ID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 2_000 {
		t.Errorf("balance = %d, want 2000", balance)
	}
}

func TestPostgresSameWagerIsIdempotentAcrossFiftyRequestsAndThreeProcesses(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: integrationPlayerID(t), InitialBalance: domain.Money{Units: 10_000, Currency: "BRL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	command := integrationBetCommand(opened.Wallet, integrationPlayerID(t), "same", 2_500)
	groups := make([][]application.ProcessWagerCommand, 3)
	for request := 0; request < 50; request++ {
		groups[request%len(groups)] = append(groups[request%len(groups)], command)
	}
	results := runPostgresWorkerProcesses(t, os.Getenv("TEST_DATABASE_URL"), groups)
	if len(results) != 50 {
		t.Fatalf("got %d results, want 50", len(results))
	}
	transactionIDs := map[string]bool{}
	for _, result := range results {
		if result.Error != "" {
			t.Errorf("worker request failed: %s", result.Error)
			continue
		}
		if domain.TransactionStatus(result.Status) != domain.TransactionProcessed {
			t.Errorf("status=%s, want PROCESSED", result.Status)
		}
		transactionIDs[result.TransactionID] = true
	}
	if len(transactionIDs) != 1 {
		t.Fatalf("distinct transaction IDs=%d, want exactly one", len(transactionIDs))
	}
	var balance int64
	if err := store.pool.QueryRow(context.Background(), "SELECT balance_minor FROM wallets WHERE id=$1", opened.Wallet.ID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 7_500 {
		t.Fatalf("balance=%d, want 7500", balance)
	}
	var transactionCount, ledgerCount, eventCount int
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM wager_transactions WHERE provider_id=$1 AND idempotency_key=$2", command.ProviderID, command.IdempotencyKey).Scan(&transactionCount); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id=$1", opened.Wallet.ID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox_events WHERE causation_id=$1", results[0].TransactionID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if transactionCount != 1 || ledgerCount != 2 || eventCount != 2 {
		t.Fatalf("transaction/ledger/wager-outbox counts=%d/%d/%d, want 1/2/2", transactionCount, ledgerCount, eventCount)
	}
	reconciliation, err := application.NewQueryService(store).Reconcile(context.Background(), opened.Wallet.ID)
	if err != nil || !reconciliation.Consistent {
		t.Fatalf("reconciliation=%+v error=%v", reconciliation, err)
	}
}

func integrationBetCommand(wallet *domain.Wallet, operationID, suffix string, amount int64) application.ProcessWagerCommand {
	return application.ProcessWagerCommand{
		WalletID: wallet.ID, PlayerID: wallet.PlayerID, ProviderID: "concurrent-provider-" + operationID,
		ExternalTransactionID: "concurrent-bet-" + suffix + "-" + operationID, IdempotencyKey: "concurrent-key-" + suffix + "-" + operationID,
		RoundID: "round-1", GameID: "game-1", Kind: domain.TransactionBet, Amount: domain.Money{Units: amount, Currency: "BRL"},
	}
}

type postgresWorkerPayload struct {
	DatabaseURL string
	Commands    []application.ProcessWagerCommand
}

type postgresWorkerOutcome struct {
	Status           string
	TransactionID    string
	FailureCode      string
	Balance          *domain.Money
	IdempotentReplay bool
	Error            string
}

func TestPostgresWorkerProcess(t *testing.T) {
	encoded := os.Getenv("TEST_POSTGRES_WORKER_PAYLOAD")
	if encoded == "" {
		return
	}
	var payload postgresWorkerPayload
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		t.Fatalf("decode worker payload: %v", err)
	}
	store, err := NewPool(context.Background(), payload.DatabaseURL)
	if err != nil {
		t.Fatalf("open independent worker pool: %v", err)
	}
	defer store.Close()
	service := application.NewProcessWagerService(store, application.UUIDGenerator{})
	start := make(chan struct{})
	outcomes := make([]postgresWorkerOutcome, len(payload.Commands))
	var group sync.WaitGroup
	for index, command := range payload.Commands {
		group.Add(1)
		go func(index int, command application.ProcessWagerCommand) {
			defer group.Done()
			<-start
			result, err := service.Execute(context.Background(), command)
			if err != nil {
				outcomes[index].Error = err.Error()
				return
			}
			outcomes[index] = postgresWorkerOutcome{Status: string(result.Status), TransactionID: result.TransactionID, FailureCode: result.FailureCode, Balance: result.Balance, IdempotentReplay: result.IdempotentReplay}
		}(index, command)
	}
	close(start)
	group.Wait()
	resultJSON, err := json.Marshal(outcomes)
	if err != nil {
		t.Fatalf("encode worker results: %v", err)
	}
	fmt.Printf("POSTGRES_WORKER_RESULT:%s\n", resultJSON)
}

func runPostgresWorkerProcesses(t *testing.T, databaseURL string, groups [][]application.ProcessWagerCommand) []postgresWorkerOutcome {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	type worker struct {
		command *exec.Cmd
		output  bytes.Buffer
	}
	workers := make([]*worker, 0, len(groups))
	for _, commands := range groups {
		payload, err := json.Marshal(postgresWorkerPayload{DatabaseURL: databaseURL, Commands: commands})
		if err != nil {
			t.Fatal(err)
		}
		child := &worker{command: exec.Command(executable, "-test.run=^TestPostgresWorkerProcess$")}
		child.command.Env = append(os.Environ(), "TEST_POSTGRES_WORKER_PAYLOAD="+string(payload))
		child.command.Stdout, child.command.Stderr = &child.output, &child.output
		if err := child.command.Start(); err != nil {
			t.Fatalf("start independent worker: %v", err)
		}
		workers = append(workers, child)
	}
	results := make([]postgresWorkerOutcome, 0)
	for _, child := range workers {
		if err := child.command.Wait(); err != nil {
			t.Fatalf("worker process failed: %v\n%s", err, child.output.String())
		}
		const marker = "POSTGRES_WORKER_RESULT:"
		output := child.output.String()
		position := strings.LastIndex(output, marker)
		if position < 0 {
			t.Fatalf("worker did not return results:\n%s", output)
		}
		lineEnd := strings.IndexByte(output[position:], '\n')
		if lineEnd >= 0 {
			output = output[position+len(marker) : position+lineEnd]
		} else {
			output = output[position+len(marker):]
		}
		var outcomes []postgresWorkerOutcome
		if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &outcomes); err != nil {
			t.Fatalf("decode worker results: %v\n%s", err, child.output.String())
		}
		results = append(results, outcomes...)
	}
	return results
}

func TestPostgresOutboxClaimsAreExclusiveAndPreserveAggregateOrder(t *testing.T) {
	store := integrationStore(t)
	// Keep records left by earlier manual integration runs out of this test's
	// eligible range, then restore their schedule when the test finishes.
	type previousSchedule struct {
		id    string
		next  time.Time
		owner *string
		until *time.Time
	}
	rows, err := store.pool.Query(context.Background(), "SELECT event_id, next_attempt_at, lease_owner, lease_until FROM outbox_events WHERE published_at IS NULL")
	if err != nil {
		t.Fatal(err)
	}
	var previous []previousSchedule
	for rows.Next() {
		var item previousSchedule
		if err := rows.Scan(&item.id, &item.next, &item.owner, &item.until); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		previous = append(previous, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	for _, item := range previous {
		if _, err := store.pool.Exec(context.Background(), "UPDATE outbox_events SET next_attempt_at = now() + interval '1 day', lease_owner = NULL, lease_until = NULL WHERE event_id = $1", item.id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, item := range previous {
			_, _ = store.pool.Exec(context.Background(), "UPDATE outbox_events SET next_attempt_at=$2, lease_owner=$3, lease_until=$4 WHERE event_id=$1 AND published_at IS NULL", item.id, item.next, item.owner, item.until)
		}
	})
	ids := application.UUIDGenerator{}
	open := application.NewOpenWalletService(store, ids)
	firstWallet, err := open.Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: domain.Money{Units: 1_000, Currency: "BRL"}})
	if err != nil {
		t.Fatal(err)
	}
	secondWallet, err := open.Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: domain.Money{Units: 2_000, Currency: "BRL"}})
	if err != nil {
		t.Fatal(err)
	}
	repository := NewOutboxDispatcherRepository(store)
	firstClaim, err := repository.Claim(context.Background(), "publisher-a", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	secondClaim, err := repository.Claim(context.Background(), "publisher-b", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, event := range firstClaim {
		seen[event.EventID] = true
	}
	for _, event := range secondClaim {
		if seen[event.EventID] {
			t.Fatalf("event %s was claimed by two publishers", event.EventID)
		}
		seen[event.EventID] = true
	}
	if len(firstClaim) != 1 || len(secondClaim) != 1 {
		t.Fatalf("claims=%d/%d, expected each publisher to claim one independent aggregate", len(firstClaim), len(secondClaim))
	}
	if firstClaim[0].AggregateID == secondClaim[0].AggregateID {
		t.Fatal("publishers claimed two ordered events from the same wallet instead of parallel wallet aggregates")
	}
	claimedAggregate := map[string]bool{firstClaim[0].AggregateID: true, secondClaim[0].AggregateID: true}
	if !claimedAggregate[firstWallet.Wallet.ID] || !claimedAggregate[secondWallet.Wallet.ID] {
		t.Fatal("independent wallet aggregates were not processed in parallel")
	}
	blocked, err := repository.Claim(context.Background(), "publisher-c", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 0 {
		t.Fatalf("later event overtook an unconfirmed aggregate head: %+v", blocked)
	}
	for _, event := range append(firstClaim, secondClaim...) {
		if err := repository.MarkPublished(context.Background(), event.EventID, eventOwner(event, firstClaim, secondClaim, "publisher-a", "publisher-b"), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	following, err := repository.Claim(context.Background(), "publisher-c", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(following) != 2 {
		t.Fatalf("following claim=%d events, want next event for each wallet", len(following))
	}
	for _, event := range following {
		if err := repository.MarkPublished(context.Background(), event.EventID, "publisher-c", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func eventOwner(event application.OutboxEvent, first, second []application.OutboxEvent, firstOwner, secondOwner string) string {
	for _, candidate := range first {
		if candidate.EventID == event.EventID {
			return firstOwner
		}
	}
	for _, candidate := range second {
		if candidate.EventID == event.EventID {
			return secondOwner
		}
	}
	return ""
}
