package application

import (
	"testing"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

func TestCanonicalPayloadHashIsStableAndExcludesTransportFields(t *testing.T) {
	command := ProcessWagerCommand{
		TransactionID: "internal-a", WalletID: "wallet-1", PlayerID: "player-1",
		ProviderID: "provider-1", ExternalTransactionID: "external-1",
		IdempotencyKey: "key-a", RoundID: "round-1", GameID: "game-1",
		Kind: domain.TransactionBet, Amount: domain.Money{Units: 2500, Currency: "BRL"},
		CorrelationID: "correlation-a",
	}
	first, err := CanonicalPayloadHash(command)
	if err != nil {
		t.Fatal(err)
	}

	command.TransactionID = "internal-b"
	command.IdempotencyKey = "key-b"
	command.CorrelationID = "correlation-b"
	second, err := CanonicalPayloadHash(command)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("hash mudou para a mesma operação de negócio com metadados diferentes")
	}

	command.Amount.Units++
	third, err := CanonicalPayloadHash(command)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Error("hash deveria mudar quando o valor financeiro muda")
	}
}
