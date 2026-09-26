package sqs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/postgres"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type failFirstDelete struct {
	*awssqs.Client
	failed bool
}

func TestLocalStackRedrivesPoisonMessageToDeadLetterQueue(t *testing.T) {
	endpoint, queueURL, dlqURL := os.Getenv("TEST_SQS_ENDPOINT"), os.Getenv("TEST_SQS_INPUT_QUEUE_URL"), os.Getenv("TEST_SQS_DLQ_URL")
	if endpoint == "" || queueURL == "" || dlqURL == "" {
		t.Skip("set TEST_SQS_ENDPOINT, TEST_SQS_INPUT_QUEUE_URL and TEST_SQS_DLQ_URL to run LocalStack DLQ integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := NewAWSClient(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	id, err := (application.UUIDGenerator{}).NewID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendMessage(ctx, &awssqs.SendMessageInput{QueueUrl: aws.String(queueURL), MessageBody: aws.String("{invalid-json"), MessageGroupId: aws.String("dlq-test"), MessageDeduplicationId: aws.String(id)})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 8; attempt++ {
		output, err := client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
			QueueUrl: aws.String(queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 2,
			AttributeNames: []types.QueueAttributeName{types.QueueAttributeName("ApproximateReceiveCount"), types.QueueAttributeName("MessageGroupId")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(output.Messages) == 0 {
			continue
		}
		message := output.Messages[0]
		if message.Attributes["ApproximateReceiveCount"] == "" || message.Attributes["MessageGroupId"] != "dlq-test" {
			t.Fatalf("SQS retry attributes missing: %+v", message.Attributes)
		}
		if _, err := client.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{QueueUrl: aws.String(queueURL), ReceiptHandle: message.ReceiptHandle, VisibilityTimeout: 0}); err != nil {
			t.Fatal(err)
		}
	}
	output, err := client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{QueueUrl: aws.String(dlqURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Messages) != 1 || aws.ToString(output.Messages[0].Body) != "{invalid-json" {
		t.Fatalf("poison message was not moved to DLQ: %+v", output.Messages)
	}
	_, _ = client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{QueueUrl: aws.String(dlqURL), ReceiptHandle: output.Messages[0].ReceiptHandle})
}

func (f *failFirstDelete) DeleteMessage(ctx context.Context, input *awssqs.DeleteMessageInput, options ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error) {
	if !f.failed {
		f.failed = true
		return nil, fmt.Errorf("simulated crash after SQL commit")
	}
	return f.Client.DeleteMessage(ctx, input, options...)
}

func TestConsumerLocalStackRedeliveryDoesNotRepeatFinancialEffects(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	endpoint := os.Getenv("TEST_SQS_ENDPOINT")
	queueURL := os.Getenv("TEST_SQS_INPUT_QUEUE_URL")
	if databaseURL == "" || endpoint == "" || queueURL == "" {
		t.Skip("set TEST_DATABASE_URL, TEST_SQS_ENDPOINT and TEST_SQS_INPUT_QUEUE_URL to run LocalStack integration")
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
	opened, err := application.NewOpenWalletService(store, ids).Execute(ctx, application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: domain.Money{Units: 10_000, Currency: "BRL"},
	})
	if err != nil {
		t.Fatal(err)
	}

	client, err := NewAWSClient(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	externalID, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	messageID, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	envelope := wagerEnvelope{MessageID: messageID, Type: "WagerTransactionRequested", OccurredAt: time.Now().UTC()}
	envelope.Data.ProviderID, envelope.Data.ExternalTransactionID, envelope.Data.IdempotencyKey = "integration-provider", externalID, "integration-key-"+externalID
	envelope.Data.PlayerID, envelope.Data.WalletID, envelope.Data.RoundID, envelope.Data.GameID = playerID, opened.Wallet.ID, "integration-round", "integration-game"
	envelope.Data.Kind, envelope.Data.Money.Amount, envelope.Data.Money.Currency = string(domain.TransactionBet), "25.00", "BRL"
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendMessage(ctx, &awssqs.SendMessageInput{QueueUrl: aws.String(queueURL), MessageBody: aws.String(string(body)), MessageGroupId: aws.String(opened.Wallet.ID), MessageDeduplicationId: aws.String(messageID)})
	if err != nil {
		t.Fatal(err)
	}
	received, err := client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{QueueUrl: aws.String(queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 5})
	if err != nil || len(received.Messages) != 1 {
		t.Fatalf("receive SQS message: count=%d err=%v", len(received.Messages), err)
	}

	processor := application.NewProcessInboxWagerService(store, application.NewProcessWagerService(store, ids))
	flakyClient := &failFirstDelete{Client: client}
	consumer, err := NewConsumer(flakyClient, processor, queueURL, "wager-api-integration")
	if err != nil {
		t.Fatal(err)
	}
	message := received.Messages[0]
	if err := consumer.handle(ctx, message); err == nil {
		t.Fatal("expected simulated delete failure after commit")
	}
	if _, err := client.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{QueueUrl: aws.String(queueURL), ReceiptHandle: message.ReceiptHandle, VisibilityTimeout: 0}); err != nil {
		t.Fatal(err)
	}
	redelivered, err := client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{QueueUrl: aws.String(queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 5})
	if err != nil || len(redelivered.Messages) != 1 {
		t.Fatalf("receive redelivery: count=%d err=%v", len(redelivered.Messages), err)
	}
	// Recreate the application service to model another process taking over.
	newProcessor := application.NewProcessInboxWagerService(store, application.NewProcessWagerService(store, ids))
	restartedConsumer, err := NewConsumer(client, newProcessor, queueURL, "wager-api-integration")
	if err != nil {
		t.Fatal(err)
	}
	if err := restartedConsumer.handle(ctx, redelivered.Messages[0]); err != nil {
		t.Fatalf("redelivery handling: %v", err)
	}

	var balance int64
	if err := pool.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id = $1", opened.Wallet.ID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 7_500 {
		t.Fatalf("balance=%d, want 7500", balance)
	}
	var inboxCount, ledgerCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM inbox_messages WHERE consumer_name=$1 AND message_id=$2", "wager-api-integration", messageID).Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id=$1", opened.Wallet.ID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 || ledgerCount != 2 {
		t.Fatalf("inbox=%d ledger=%d, want one inbox receipt and opening+bet ledger", inboxCount, ledgerCount)
	}
}
