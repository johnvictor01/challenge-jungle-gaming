package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/platform"
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
		InitialBalance: testMoney(10_000, "BRL"),
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	process := application.NewProcessWagerService(store, ids)
	command := application.ProcessWagerCommand{
		WalletID: opened.Wallet.ID(), PlayerID: opened.Wallet.PlayerID(), ProviderID: "integration-provider-" + operationID,
		ExternalTransactionID: "integration-bet-" + operationID, IdempotencyKey: "integration-idem-" + operationID,
		RoundID: "round-1", GameID: "game-1", Kind: domain.TransactionBet,
		Amount: testMoney(2_500, "BRL"),
	}
	result, err := process.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("process wager: %v", err)
	}
	if result.Status != domain.TransactionProcessed || result.Balance == nil || result.Balance.Units() != 7_500 {
		t.Fatalf("unexpected result: %+v", result)
	}
	replay, err := process.Execute(context.Background(), command)
	if err != nil || !replay.IdempotentReplay || replay.Balance == nil || replay.Balance.Units() != 7_500 {
		t.Fatalf("unexpected persisted replay: result=%+v error=%v", replay, err)
	}

	var balance int64
	var version int64
	if err := store.pool.QueryRow(context.Background(), "SELECT balance_minor, version FROM wallets WHERE id = $1", opened.Wallet.ID()).Scan(&balance, &version); err != nil {
		t.Fatal(err)
	}
	if balance != 7_500 || version != 2 {
		t.Errorf("wallet = balance:%d version:%d, want 7500/2", balance, version)
	}
	var ledgerCount, eventCount int
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id = $1", opened.Wallet.ID()).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox_events WHERE aggregate_id = $1", opened.Wallet.ID()).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 || eventCount != 4 {
		t.Errorf("ledger/events = %d/%d, want 2/4", ledgerCount, eventCount)
	}
	queries := application.NewQueryService(store)
	reconciliation, err := queries.Reconcile(context.Background(), opened.Wallet.ID())
	if err != nil || !reconciliation.Consistent || reconciliation.CheckedEntries != 2 {
		t.Errorf("reconciliation = %+v, error=%v", reconciliation, err)
	}
	firstPage, err := queries.Ledger(context.Background(), opened.Wallet.ID(), "", 1)
	if err != nil || len(firstPage.Entries) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("first ledger page=%+v error=%v", firstPage, err)
	}
	secondPage, err := queries.Ledger(context.Background(), opened.Wallet.ID(), firstPage.NextCursor, 1)
	if err != nil || len(secondPage.Entries) != 1 || firstPage.Entries[0].ID() == secondPage.Entries[0].ID() {
		t.Fatalf("second ledger page=%+v error=%v", secondPage, err)
	}
}

func TestPostgresLedgerIsAppendOnly(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{
		PlayerID:       integrationPlayerID(t),
		InitialBalance: testMoney(1_000, "BRL"),
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	_, err = store.pool.Exec(context.Background(), "DELETE FROM wallet_ledger_entries WHERE wallet_id = $1", opened.Wallet.ID())
	if err == nil {
		t.Fatal("DELETE ledger entry succeeded, want append-only trigger error")
	}
}

func TestPostgresEnforcesWalletUniquenessAndProviderIsolation(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	playerID := integrationPlayerID(t)
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: testMoney(1_000, "BRL"),
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	_, err = application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: testMoney(0, "BRL"),
	})
	if err != application.ErrWalletAlreadyExists {
		t.Fatalf("duplicate wallet error = %v, want ErrWalletAlreadyExists", err)
	}
	process := application.NewProcessWagerService(store, ids)
	_, err = process.Execute(context.Background(), application.ProcessWagerCommand{
		WalletID: opened.Wallet.ID(), PlayerID: opened.Wallet.PlayerID(), ProviderID: "provider-owned-" + playerID,
		ExternalTransactionID: "provider-tx-" + playerID, IdempotencyKey: "provider-key-" + playerID,
		RoundID: "round-1", GameID: "game-1", Kind: domain.TransactionLoss,
		Amount: testMoney(0, "BRL"),
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
		InitialBalance: testMoney(10_000, "BRL"),
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	commandA := integrationBetCommand(opened.Wallet, operationID, "a", 8_000)
	commandB := integrationBetCommand(opened.Wallet, operationID, "b", 8_000)
	results := runPostgresWorkerProcesses(t, os.Getenv("TEST_DATABASE_URL"), [][]application.ProcessWagerCommand{{commandA}, {commandB}, {commandA}})
	var processed, rejected int
	byTransactionID := map[string][]domain.TransactionStatus{}
	for _, result := range results {
		if result.Error != "" {
			t.Errorf("worker operation failed: %s", result.Error)
			continue
		}
		byTransactionID[result.TransactionID] = append(byTransactionID[result.TransactionID], domain.TransactionStatus(result.Status))
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
	if processed+rejected != 3 || (processed != 1 && processed != 2) {
		t.Fatalf("results processed/rejected = %d/%d, want exactly one debit and its possible idempotent replay", processed, rejected)
	}
	duplicateReplayConsistent := false
	for _, statuses := range byTransactionID {
		if len(statuses) == 2 && statuses[0] == statuses[1] {
			duplicateReplayConsistent = true
		}
	}
	if !duplicateReplayConsistent {
		t.Fatalf("duplicate operation did not return one consistent persisted outcome: %+v", byTransactionID)
	}
	var balance int64
	if err := store.pool.QueryRow(context.Background(), "SELECT balance_minor FROM wallets WHERE id = $1", opened.Wallet.ID()).Scan(&balance); err != nil {
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
		PlayerID: integrationPlayerID(t), InitialBalance: testMoney(10_000, "BRL"),
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
	if err := store.pool.QueryRow(context.Background(), "SELECT balance_minor FROM wallets WHERE id=$1", opened.Wallet.ID()).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 7_500 {
		t.Fatalf("balance=%d, want 7500", balance)
	}
	var transactionCount, ledgerCount, eventCount int
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM wager_transactions WHERE provider_id=$1 AND idempotency_key=$2", command.ProviderID, command.IdempotencyKey).Scan(&transactionCount); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id=$1", opened.Wallet.ID()).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox_events WHERE causation_id=$1", results[0].TransactionID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if transactionCount != 1 || ledgerCount != 2 || eventCount != 2 {
		t.Fatalf("transaction/ledger/wager-outbox counts=%d/%d/%d, want 1/2/2", transactionCount, ledgerCount, eventCount)
	}
	reconciliation, err := application.NewQueryService(store).Reconcile(context.Background(), opened.Wallet.ID())
	if err != nil || !reconciliation.Consistent {
		t.Fatalf("reconciliation=%+v error=%v", reconciliation, err)
	}
}

func TestPostgresDifferentWalletsProcessInParallelAndReconcileWithLedger(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	open := application.NewOpenWalletService(store, ids)
	first, err := open.Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: testMoney(10_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := open.Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: testMoney(20_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	commands := [][]application.ProcessWagerCommand{{integrationBetCommand(first.Wallet, integrationPlayerID(t), "first-wallet", 2_000)}, {integrationBetCommand(second.Wallet, integrationPlayerID(t), "second-wallet", 3_000)}}
	results := runPostgresWorkerProcesses(t, os.Getenv("TEST_DATABASE_URL"), commands)
	if len(results) != 2 {
		t.Fatalf("got %d results, want two", len(results))
	}
	for _, result := range results {
		if result.Error != "" || domain.TransactionStatus(result.Status) != domain.TransactionProcessed {
			t.Errorf("wallet operation result = %+v", result)
		}
	}
	queries := application.NewQueryService(store)
	for _, expected := range []struct {
		walletID string
		balance  int64
		entries  int64
	}{{first.Wallet.ID(), 8_000, 2}, {second.Wallet.ID(), 17_000, 2}} {
		reconciliation, err := queries.Reconcile(context.Background(), expected.walletID)
		if err != nil || !reconciliation.Consistent || reconciliation.StoredBalance.Units() != expected.balance || reconciliation.CalculatedBalance.Units() != expected.balance || reconciliation.CheckedEntries != expected.entries {
			t.Errorf("reconciliation for wallet %s = %+v error=%v, want balance=%d entries=%d", expected.walletID, reconciliation, err, expected.balance, expected.entries)
		}
	}
}

func TestPostgresConcurrentRefundAndRollbackApplyOnlyOneReversal(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	operationID := integrationPlayerID(t)
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: testMoney(10_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	bet := integrationBetCommand(opened.Wallet, operationID, "reference-bet", 2_500)
	betResult, err := application.NewProcessWagerService(store, ids).Execute(context.Background(), bet)
	if err != nil || betResult.Status != domain.TransactionProcessed {
		t.Fatalf("reference bet = %+v error=%v", betResult, err)
	}
	refund := integrationBetCommand(opened.Wallet, operationID, "refund", 2_500)
	refund.Kind, refund.ReferenceExternalTransactionID = domain.TransactionRefund, bet.ExternalTransactionID
	rollback := integrationBetCommand(opened.Wallet, operationID, "rollback", 2_500)
	rollback.Kind, rollback.ReferenceExternalTransactionID = domain.TransactionRollback, bet.ExternalTransactionID
	results := runPostgresWorkerProcesses(t, os.Getenv("TEST_DATABASE_URL"), [][]application.ProcessWagerCommand{{refund}, {rollback}, {refund}})
	processed, rejected := 0, 0
	byTransactionID := map[string][]domain.TransactionStatus{}
	for _, result := range results {
		if result.Error != "" {
			t.Errorf("concurrent reversal returned error: %s (result=%+v)", result.Error, result)
			continue
		}
		byTransactionID[result.TransactionID] = append(byTransactionID[result.TransactionID], domain.TransactionStatus(result.Status))
		switch domain.TransactionStatus(result.Status) {
		case domain.TransactionProcessed:
			processed++
		case domain.TransactionRejected:
			rejected++
		default:
			t.Errorf("unexpected reversal result: %+v", result)
		}
	}
	if processed+rejected != 3 || processed < 1 || processed > 2 {
		t.Fatalf("reversal outcomes processed/rejected=%d/%d, want one effective reversal and its consistent replay", processed, rejected)
	}
	duplicateReplayConsistent := false
	for _, statuses := range byTransactionID {
		if len(statuses) == 2 && statuses[0] == statuses[1] {
			duplicateReplayConsistent = true
		}
	}
	if !duplicateReplayConsistent {
		t.Fatalf("duplicate reversal did not return its persisted outcome: %+v", byTransactionID)
	}
	var reversals int
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM wager_transactions WHERE wallet_id=$1 AND kind IN ('REFUND','ROLLBACK') AND status='PROCESSED'", opened.Wallet.ID()).Scan(&reversals); err != nil {
		t.Fatal(err)
	}
	if reversals != 1 {
		t.Fatalf("processed reversal count=%d, want 1", reversals)
	}
	reconciliation, err := application.NewQueryService(store).Reconcile(context.Background(), opened.Wallet.ID())
	if err != nil || !reconciliation.Consistent || reconciliation.StoredBalance.Units() != 10_000 || reconciliation.CheckedEntries != 3 {
		t.Fatalf("reconciliation=%+v error=%v, want balance 10000 and three ledger entries", reconciliation, err)
	}
}

func TestPostgresEnforcesWalletCurrencyAndExternalIdempotencyConstraints(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	playerID := integrationPlayerID(t)
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{PlayerID: playerID, InitialBalance: testMoney(5_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	duplicateID, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(context.Background(), `INSERT INTO wallets (id, player_id, currency, balance_minor, version, created_at, updated_at)
		VALUES ($1,$2,'BRL',0,1,now(),now())`, duplicateID, playerID)
	assertUniqueConstraint(t, err, "wallet player/currency unique index")
	_, err = store.pool.Exec(context.Background(), "UPDATE wallets SET balance_minor=-1 WHERE id=$1", opened.Wallet.ID())
	assertPostgresConstraintCode(t, err, "23514", "wallet non-negative balance check")

	command := integrationBetCommand(opened.Wallet, integrationPlayerID(t), "unique-source", 500)
	if _, err := application.NewProcessWagerService(store, ids).Execute(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	duplicateTransactionID, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(context.Background(), `INSERT INTO wager_transactions (
		id, origin, wallet_id, player_id, currency, provider_id, external_transaction_id, idempotency_key, payload_hash,
		kind, amount_minor, round_id, game_id, status, failure_code, result_balance_minor, result_currency, attempt_count, created_at, updated_at, processed_at)
		SELECT $1, origin, wallet_id, player_id, currency, provider_id, 'different-external-id', idempotency_key, payload_hash,
		kind, amount_minor, round_id, game_id, status, failure_code, result_balance_minor, result_currency, attempt_count, created_at, updated_at, processed_at
		FROM wager_transactions WHERE provider_id=$2 AND idempotency_key=$3`, duplicateTransactionID, command.ProviderID, command.IdempotencyKey)
	assertUniqueConstraint(t, err, "provider/idempotency unique index")
}

func TestPostgresRejectedReversalDoesNotReserveReference(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	operationID := integrationPlayerID(t)
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: testMoney(10_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewProcessWagerService(store, ids)
	invalidRefund := integrationBetCommand(opened.Wallet, operationID, "invalid-refund", 1_000)
	invalidRefund.Kind = domain.TransactionRefund
	invalidRefund.ReferenceExternalTransactionID = "late-bet-" + operationID
	pending, err := service.Execute(context.Background(), invalidRefund)
	if err != nil || pending.Status != domain.TransactionPendingReference {
		t.Fatalf("invalid refund initial state = %+v error=%v", pending, err)
	}
	bet := integrationBetCommand(opened.Wallet, operationID, "late-bet", 2_500)
	bet.ExternalTransactionID = invalidRefund.ReferenceExternalTransactionID
	if _, err := service.Execute(context.Background(), bet); err != nil {
		t.Fatal(err)
	}
	resolver := application.NewResolvePendingReferenceService(store, ids, application.ReferenceRetryPolicy{MaxAttempts: 3, RetryDelay: func(int) time.Duration { return 0 }})
	if _, err := resolver.Execute(context.Background(), pending.TransactionID); err != nil {
		t.Fatal(err)
	}
	var status, failure string
	if err := store.pool.QueryRow(context.Background(), "SELECT status, failure_code FROM wager_transactions WHERE id=$1", pending.TransactionID).Scan(&status, &failure); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.TransactionRejected) || failure != "INVALID_REFERENCE" {
		t.Fatalf("invalid refund status/failure=%s/%s", status, failure)
	}
	validRefund := integrationBetCommand(opened.Wallet, operationID, "valid-refund", 2_500)
	validRefund.Kind = domain.TransactionRefund
	validRefund.ReferenceExternalTransactionID = bet.ExternalTransactionID
	result, err := service.Execute(context.Background(), validRefund)
	if err != nil || result.Status != domain.TransactionProcessed {
		t.Fatalf("valid refund after rejected one = %+v error=%v", result, err)
	}
}

func assertUniqueConstraint(t *testing.T, err error, constraint string) {
	t.Helper()
	assertPostgresConstraintCode(t, err, "23505", constraint)
}

func assertPostgresConstraintCode(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != code {
		t.Fatalf("%s error = %v, want PostgreSQL constraint violation %s", constraint, err, code)
	}
}

func TestPostgresPendingReferenceResumesAfterServiceRestart(t *testing.T) {
	store := integrationStore(t)
	ids := application.UUIDGenerator{}
	opened, err := application.NewOpenWalletService(store, ids).Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: testMoney(5_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	operationID := integrationPlayerID(t)
	pending := application.ProcessWagerCommand{
		WalletID: opened.Wallet.ID(), PlayerID: opened.Wallet.PlayerID(), ProviderID: "pending-provider-" + operationID,
		ExternalTransactionID: "late-refund-" + operationID, IdempotencyKey: "late-refund-key-" + operationID,
		RoundID: "round-pending", GameID: "game-pending", Kind: domain.TransactionRefund,
		Amount: testMoney(2_500, "BRL"), ReferenceExternalTransactionID: "late-bet-" + operationID,
	}
	pendingResult, err := application.NewProcessWagerService(store, ids).Execute(context.Background(), pending)
	if err != nil || pendingResult.Status != domain.TransactionPendingReference {
		t.Fatalf("initial operation = %+v error=%v, want PENDING_REFERENCE", pendingResult, err)
	}
	// Encerrar o primeiro pool representa a parada do processo: a retomada só
	// pode depender dos dados persistidos, não do estado em memória.
	store.Close()
	restartedStore, err := NewPool(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer restartedStore.Close()
	bet := application.ProcessWagerCommand{
		WalletID: opened.Wallet.ID(), PlayerID: opened.Wallet.PlayerID(), ProviderID: pending.ProviderID,
		ExternalTransactionID: pending.ReferenceExternalTransactionID, IdempotencyKey: "late-bet-key-" + operationID,
		RoundID: pending.RoundID, GameID: "game-pending", Kind: domain.TransactionBet,
		Amount: testMoney(2_500, "BRL"),
	}
	betResult, err := application.NewProcessWagerService(restartedStore, ids).Execute(context.Background(), bet)
	if err != nil || betResult.Status != domain.TransactionProcessed {
		t.Fatalf("reference operation = %+v error=%v, want PROCESSED", betResult, err)
	}
	resolver := application.NewResolvePendingReferenceService(restartedStore, ids, application.ReferenceRetryPolicy{})
	worker, err := platform.NewPendingReferenceWorker(restartedStore, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	resolved, err := application.NewQueryService(restartedStore).Transaction(context.Background(), pendingResult.TransactionID)
	if err != nil || resolved.Status() != domain.TransactionProcessed || resolved.ResultBalance() == nil || resolved.ResultBalance().Units() != 5_000 {
		t.Fatalf("resumed operation = %+v error=%v, want PROCESSED with balance 5000", resolved, err)
	}
	reconciliation, err := application.NewQueryService(restartedStore).Reconcile(context.Background(), opened.Wallet.ID())
	if err != nil || !reconciliation.Consistent || reconciliation.StoredBalance.Units() != 5_000 || reconciliation.CalculatedBalance.Units() != 5_000 || reconciliation.CheckedEntries != 3 {
		t.Fatalf("reconciliation=%+v error=%v, want consistent balance 5000 with opening, bet, and refund entries", reconciliation, err)
	}
}

func integrationBetCommand(wallet *domain.Wallet, operationID, suffix string, amount int64) application.ProcessWagerCommand {
	return application.ProcessWagerCommand{
		WalletID: wallet.ID(), PlayerID: wallet.PlayerID(), ProviderID: "concurrent-provider-" + operationID,
		ExternalTransactionID: "concurrent-bet-" + suffix + "-" + operationID, IdempotencyKey: "concurrent-key-" + suffix + "-" + operationID,
		RoundID: "round-1", GameID: "game-1", Kind: domain.TransactionBet, Amount: testMoney(amount, "BRL"),
	}
}

type postgresWorkerPayload struct {
	DatabaseURL string
	Commands    []postgresWorkerCommand
}

// postgresWorkerCommand is the JSON-safe form used to pass commands to the
// subprocess. Money intentionally keeps its fields private, so the test wire
// format spells out its minimal representation and rebuilds the domain value.
type postgresWorkerCommand struct {
	WalletID                       string
	PlayerID                       string
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	RoundID                        string
	GameID                         string
	Kind                           domain.TransactionKind
	AmountUnits                    int64
	AmountCurrency                 string
	ReferenceExternalTransactionID string
	TransactionID                  string
	PayloadHash                    string
	CorrelationID                  string
}

func workerCommandFrom(command application.ProcessWagerCommand) postgresWorkerCommand {
	return postgresWorkerCommand{
		WalletID: command.WalletID, PlayerID: command.PlayerID, ProviderID: command.ProviderID,
		ExternalTransactionID: command.ExternalTransactionID, IdempotencyKey: command.IdempotencyKey,
		RoundID: command.RoundID, GameID: command.GameID, Kind: command.Kind,
		AmountUnits: command.Amount.Units(), AmountCurrency: command.Amount.Currency(),
		ReferenceExternalTransactionID: command.ReferenceExternalTransactionID,
		TransactionID:                  command.TransactionID, PayloadHash: command.PayloadHash, CorrelationID: command.CorrelationID,
	}
}

func (command postgresWorkerCommand) processWagerCommand() (application.ProcessWagerCommand, error) {
	amount, err := domain.NewMoney(command.AmountUnits, command.AmountCurrency)
	if err != nil {
		return application.ProcessWagerCommand{}, err
	}
	return application.ProcessWagerCommand{
		WalletID: command.WalletID, PlayerID: command.PlayerID, ProviderID: command.ProviderID,
		ExternalTransactionID: command.ExternalTransactionID, IdempotencyKey: command.IdempotencyKey,
		RoundID: command.RoundID, GameID: command.GameID, Kind: command.Kind, Amount: amount,
		ReferenceExternalTransactionID: command.ReferenceExternalTransactionID,
		TransactionID:                  command.TransactionID, PayloadHash: command.PayloadHash, CorrelationID: command.CorrelationID,
	}, nil
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
	for index, wireCommand := range payload.Commands {
		command, err := wireCommand.processWagerCommand()
		if err != nil {
			t.Fatalf("rebuild worker command[%d]: %v", index, err)
		}
		group.Add(1)
		go func(index int, command application.ProcessWagerCommand) {
			defer group.Done()
			<-start
			result, err := service.Execute(context.Background(), command)
			if err != nil {
				outcomes[index].Error = fmt.Sprintf("command[%d]: %v", index, err)
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
		wireCommands := make([]postgresWorkerCommand, len(commands))
		for index, command := range commands {
			wireCommands[index] = workerCommandFrom(command)
		}
		payload, err := json.Marshal(postgresWorkerPayload{DatabaseURL: databaseURL, Commands: wireCommands})
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
	firstWallet, err := open.Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: testMoney(1_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	secondWallet, err := open.Execute(context.Background(), application.OpenWalletCommand{PlayerID: integrationPlayerID(t), InitialBalance: testMoney(2_000, "BRL")})
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
	if !claimedAggregate[firstWallet.Wallet.ID()] || !claimedAggregate[secondWallet.Wallet.ID()] {
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
