package application

import (
	"context"
	"errors"
	"testing"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

func TestProcessInboxWagerCommitsInboxAndWagerAndTreatsRedeliveryAsDuplicate(t *testing.T) {
	uow := &memoryUnitOfWork{state: newMemoryState()}
	ids := &sequenceIDGenerator{}
	opened, err := NewOpenWalletService(uow, ids).Execute(context.Background(), OpenWalletCommand{
		PlayerID: "player-inbox", InitialBalance: testMoney(10_000, "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewProcessInboxWagerService(uow, NewProcessWagerService(uow, ids))
	command := InboxWagerCommand{ConsumerName: "wager-api", MessageID: "message-1", Wager: ProcessWagerCommand{
		WalletID: opened.Wallet.ID(), PlayerID: opened.Wallet.PlayerID(), ProviderID: "provider-inbox",
		ExternalTransactionID: "external-inbox", IdempotencyKey: "idem-inbox", RoundID: "round-inbox", GameID: "game-inbox",
		Kind: domain.TransactionBet, Amount: testMoney(2_500, "BRL"),
	}}
	first, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if first.Wager.Status != domain.TransactionProcessed || first.DuplicateDelivery {
		t.Fatalf("first result: %+v", first)
	}
	second, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !second.DuplicateDelivery || second.Wager.TransactionID != first.Wager.TransactionID {
		t.Fatalf("redelivery result: %+v", second)
	}
	message, ok := uow.state.inbox["wager-api:message-1"]
	if !ok || message.CompletedAt == nil || message.TransactionID != first.Wager.TransactionID {
		t.Fatalf("inbox receipt not completed atomically: %+v", message)
	}
	wallet := uow.state.wallets[opened.Wallet.ID()]
	if wallet.Balance().Units() != 7_500 || len(uow.state.ledger) != 2 {
		t.Fatalf("duplicate delivery repeated financial effects: balance=%d ledger=%d", wallet.Balance().Units(), len(uow.state.ledger))
	}
}

func TestProcessInboxWagerRejectsMessageIDReusedWithDifferentPayload(t *testing.T) {
	uow := &memoryUnitOfWork{state: newMemoryState()}
	ids := &sequenceIDGenerator{}
	opened, err := NewOpenWalletService(uow, ids).Execute(context.Background(), OpenWalletCommand{PlayerID: "player-2", InitialBalance: testMoney(10_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	service := NewProcessInboxWagerService(uow, NewProcessWagerService(uow, ids))
	command := InboxWagerCommand{ConsumerName: "consumer", MessageID: "same-id", Wager: ProcessWagerCommand{
		WalletID: opened.Wallet.ID(), PlayerID: opened.Wallet.PlayerID(), ProviderID: "provider", ExternalTransactionID: "external",
		IdempotencyKey: "key", RoundID: "round", GameID: "game", Kind: domain.TransactionBet, Amount: testMoney(100, "BRL"),
	}}
	if _, err := service.Execute(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	command.Wager.Amount = testMoney(command.Wager.Amount.Units()+1, command.Wager.Amount.Currency())
	if _, err := service.Execute(context.Background(), command); !errors.Is(err, ErrInboxPayloadConflict) {
		t.Fatalf("error = %v, want ErrInboxPayloadConflict", err)
	}
}

func TestProcessInboxWagerRollsBackFinancialEffectsWhenInboxCompletionFails(t *testing.T) {
	uow := &memoryUnitOfWork{state: newMemoryState()}
	ids := &sequenceIDGenerator{}
	opened, err := NewOpenWalletService(uow, ids).Execute(context.Background(), OpenWalletCommand{PlayerID: "player-rollback", InitialBalance: testMoney(10_000, "BRL")})
	if err != nil {
		t.Fatal(err)
	}
	uow.state.failOn = "inbox_complete"
	service := NewProcessInboxWagerService(uow, NewProcessWagerService(uow, ids))
	command := InboxWagerCommand{ConsumerName: "consumer", MessageID: "message-rollback", Wager: ProcessWagerCommand{
		WalletID: opened.Wallet.ID(), PlayerID: opened.Wallet.PlayerID(), ProviderID: "provider", ExternalTransactionID: "external-rollback",
		IdempotencyKey: "key-rollback", RoundID: "round", GameID: "game", Kind: domain.TransactionBet, Amount: testMoney(100, "BRL"),
	}}
	if _, err := service.Execute(context.Background(), command); err == nil {
		t.Fatal("expected injected inbox completion failure")
	}
	if uow.state.wallets[opened.Wallet.ID()].Balance().Units() != 10_000 || len(uow.state.ledger) != 1 || len(uow.state.inbox) != 0 {
		t.Fatalf("partial transaction survived rollback: balance=%d ledger=%d inbox=%d", uow.state.wallets[opened.Wallet.ID()].Balance().Units(), len(uow.state.ledger), len(uow.state.inbox))
	}
}
