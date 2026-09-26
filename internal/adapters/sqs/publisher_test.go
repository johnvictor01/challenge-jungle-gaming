package sqs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
)

type fakeSQSClient struct {
	input *sqs.SendMessageInput
	err   error
}

func (f *fakeSQSClient) SendMessage(_ context.Context, input *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	f.input = input
	return &sqs.SendMessageOutput{}, f.err
}

func TestPublisherSendsStableEventIDAndAggregateGroup(t *testing.T) {
	client := &fakeSQSClient{}
	publisher, err := NewPublisher(client, "http://localhost/queue.fifo", "fallback")
	if err != nil {
		t.Fatal(err)
	}
	event := application.OutboxEvent{EventID: "event-id", EventType: "WalletBalanceChanged", AggregateID: "wallet-id"}
	if err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if got := aws.ToString(client.input.MessageDeduplicationId); got != event.EventID {
		t.Fatalf("dedupe id = %q", got)
	}
	if got := aws.ToString(client.input.MessageGroupId); got != event.AggregateID {
		t.Fatalf("group id = %q", got)
	}
	var decoded application.OutboxEvent
	if err := json.Unmarshal([]byte(aws.ToString(client.input.MessageBody)), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.EventID != event.EventID {
		t.Fatalf("message body event id = %q", decoded.EventID)
	}
}
