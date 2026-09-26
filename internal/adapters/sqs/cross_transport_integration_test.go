package sqs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5/pgxpool"
	httpadapter "github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/http"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/postgres"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type integrationAuthenticator struct{}

func (integrationAuthenticator) Authenticate(context.Context, string) (httpadapter.Principal, error) {
	return httpadapter.Principal{Subject: "integration", ProviderID: "provider-a", Roles: map[string]struct{}{"wager:write": {}}}, nil
}

// TestSameOperationAcrossHTTPAndSQS proves both transports share durable
// financial idempotency: HTTP commits the wager, then LocalStack delivers the
// equivalent command and the inbox records it without applying another debit.
func TestSameOperationAcrossHTTPAndSQS(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	endpoint, queueURL := os.Getenv("TEST_SQS_ENDPOINT"), os.Getenv("TEST_SQS_INPUT_QUEUE_URL")
	if databaseURL == "" || endpoint == "" || queueURL == "" {
		t.Skip("set TEST_DATABASE_URL, TEST_SQS_ENDPOINT and TEST_SQS_INPUT_QUEUE_URL to run cross-transport integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ids := application.UUIDGenerator{}
	playerID, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	walletResult, err := application.NewOpenWalletService(store, ids).Execute(ctx, application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: domain.Money{Units: 10_000, Currency: "BRL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	externalID, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	key := "cross-transport-" + externalID
	messageID, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	command := application.ProcessWagerCommand{
		WalletID: walletResult.Wallet.ID, PlayerID: playerID, ProviderID: "provider-a",
		ExternalTransactionID: externalID, IdempotencyKey: key, RoundID: "round-cross",
		GameID: "game-cross", Kind: domain.TransactionBet,
		Amount: domain.Money{Units: 1_000, Currency: "BRL"}, CorrelationID: "http-correlation",
	}
	processor := application.NewProcessWagerService(store, ids)
	api := httpadapter.NewHandler(
		application.NewOpenWalletService(store, ids), processor,
		application.NewQueryService(store), integrationAuthenticator{}, nil, nil,
	)
	body, err := json.Marshal(map[string]any{
		"providerId": "provider-a", "externalTransactionId": externalID, "playerId": playerID,
		"walletId": walletResult.Wallet.ID, "roundId": "round-cross", "gameId": "game-cross",
		"kind": "BET", "money": map[string]string{"amount": "10.00", "currency": "BRL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer integration-token")
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP wager status=%d body=%s", response.Code, response.Body.String())
	}

	envelope := wagerEnvelope{MessageID: messageID, Type: "WagerTransactionRequested", OccurredAt: time.Now().UTC()}
	envelope.Data.ProviderID, envelope.Data.ExternalTransactionID = command.ProviderID, command.ExternalTransactionID
	envelope.Data.IdempotencyKey, envelope.Data.PlayerID, envelope.Data.WalletID = key, playerID, walletResult.Wallet.ID
	envelope.Data.RoundID, envelope.Data.GameID, envelope.Data.Kind = command.RoundID, command.GameID, string(command.Kind)
	envelope.Data.Money.Amount, envelope.Data.Money.Currency = "10.00", "BRL"
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewAWSClient(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl: aws.String(queueURL), MessageBody: aws.String(string(encoded)),
		MessageGroupId: aws.String(walletResult.Wallet.ID), MessageDeduplicationId: aws.String(messageID),
	})
	if err != nil {
		t.Fatal(err)
	}
	received, err := client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
		QueueUrl: aws.String(queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 5,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeName("MessageGroupId"), types.QueueAttributeName("ApproximateReceiveCount")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("expected SQS message, got %d", len(received.Messages))
	}
	consumer, err := NewConsumer(client, application.NewProcessInboxWagerService(store, processor), queueURL, "cross-transport-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.handle(ctx, received.Messages[0]); err != nil {
		t.Fatal(err)
	}

	wallet, err := application.NewQueryService(store).Wallet(ctx, walletResult.Wallet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Balance.Units != 9_000 || wallet.Version != 2 {
		t.Fatalf("wallet after cross-transport replay = balance %d version %d; want 9000/2", wallet.Balance.Units, wallet.Version)
	}
	page, err := application.NewQueryService(store).Ledger(ctx, wallet.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("ledger entry count=%d, want opening plus one bet", len(page.Entries))
	}
	transaction, err := application.NewQueryService(store).ProviderTransaction(ctx, "provider-a", externalID)
	if err != nil {
		t.Fatal(err)
	}
	if transaction.Status != domain.TransactionProcessed || transaction.Amount.Units != command.Amount.Units {
		t.Fatalf("transaction was unexpectedly changed: %+v", transaction)
	}
	var inboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inbox_messages WHERE consumer_name = $1 AND message_id = $2`, "cross-transport-test", messageID).Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatal(fmt.Sprintf("inbox records=%d, want 1", inboxCount))
	}
}
