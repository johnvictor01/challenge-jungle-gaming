package sqs

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
)

type fakeConsumerClient struct {
	deleted    int
	deleteErr  error
	visibility int32
}

func (f *fakeConsumerClient) ChangeMessageVisibility(_ context.Context, input *sqs.ChangeMessageVisibilityInput, _ ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	f.visibility = input.VisibilityTimeout
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

func (f *fakeConsumerClient) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{}, nil
}
func (f *fakeConsumerClient) DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	f.deleted++
	return &sqs.DeleteMessageOutput{}, nil
}

type fakeInboxProcessor struct {
	err     error
	calls   int
	command application.InboxWagerCommand
}

func (f *fakeInboxProcessor) Execute(_ context.Context, command application.InboxWagerCommand) (application.InboxWagerResult, error) {
	f.calls++
	f.command = command
	return application.InboxWagerResult{}, f.err
}

const validWagerMessage = `{"messageId":"stable-message-id","type":"WagerTransactionRequested","occurredAt":"2026-09-08T12:00:00Z","data":{"providerId":"provider-a","externalTransactionId":"transaction-1","idempotencyKey":"provider-a:transaction-1","playerId":"player-a","walletId":"wallet-a","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"25.00","currency":"BRL"}}}`

func TestConsumerDeletesOnlyAfterTransactionalProcessorSucceeds(t *testing.T) {
	client, processor := &fakeConsumerClient{}, &fakeInboxProcessor{}
	consumer, err := NewConsumer(client, processor, "queue-url", "wager-api")
	if err != nil {
		t.Fatal(err)
	}
	message := types.Message{Body: aws.String(validWagerMessage), ReceiptHandle: aws.String("receipt")}
	if err := consumer.handle(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if client.deleted != 1 || processor.calls != 1 {
		t.Fatalf("deleted=%d processor calls=%d", client.deleted, processor.calls)
	}
	if processor.command.MessageID != "stable-message-id" || processor.command.Wager.IdempotencyKey != "provider-a:transaction-1" || processor.command.Wager.Amount.Units() != 2500 {
		t.Fatalf("unexpected inbox command: %+v", processor.command)
	}
}

func TestConsumerLeavesMessageWhenDurableProcessingFails(t *testing.T) {
	client, processor := &fakeConsumerClient{}, &fakeInboxProcessor{err: errors.New("postgres unavailable")}
	consumer, err := NewConsumer(client, processor, "queue-url", "wager-api")
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.handle(context.Background(), types.Message{Body: aws.String(validWagerMessage), ReceiptHandle: aws.String("receipt")}); err == nil {
		t.Fatal("expected processing error")
	}
	if client.deleted != 0 {
		t.Fatalf("message was deleted %d times after failure", client.deleted)
	}
	consumer.handleBatch(context.Background(), []types.Message{{Body: aws.String(validWagerMessage), ReceiptHandle: aws.String("receipt"), Attributes: map[string]string{"ApproximateReceiveCount": "1"}}})
	if client.visibility != 1 {
		t.Fatalf("first retry visibility=%d, want 1 second", client.visibility)
	}
}

func TestConsumerRejectsUnsupportedAndMalformedMessages(t *testing.T) {
	client, processor := &fakeConsumerClient{}, &fakeInboxProcessor{}
	consumer, err := NewConsumer(client, processor, "queue-url", "wager-api")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"{", `{"messageId":"id","type":"Other"}`} {
		if err := consumer.handle(context.Background(), types.Message{Body: aws.String(body), ReceiptHandle: aws.String("receipt")}); err == nil {
			t.Errorf("body %q should fail", body)
		}
	}
	if client.deleted != 0 || processor.calls != 0 {
		t.Fatalf("invalid messages caused side effects: deleted=%d processed=%d", client.deleted, processor.calls)
	}
}

func TestConsumerStopsPollingWhenContextIsCanceled(t *testing.T) {
	consumer, err := NewConsumer(&fakeConsumerClient{}, &fakeInboxProcessor{}, "queue-url", "wager-api")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := consumer.Run(ctx); err != nil {
		t.Fatalf("Run() after cancellation = %v", err)
	}
}
