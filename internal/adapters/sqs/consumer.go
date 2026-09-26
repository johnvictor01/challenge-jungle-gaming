package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type ReceiveDeleteAPI interface {
	ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(context.Context, *sqs.ChangeMessageVisibilityInput, ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
}

type InboxWagerProcessor interface {
	Execute(context.Context, application.InboxWagerCommand) (application.InboxWagerResult, error)
}

type wagerEnvelope struct {
	MessageID  string    `json:"messageId"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurredAt"`
	Data       struct {
		ProviderID            string `json:"providerId"`
		ExternalTransactionID string `json:"externalTransactionId"`
		IdempotencyKey        string `json:"idempotencyKey"`
		PlayerID              string `json:"playerId"`
		WalletID              string `json:"walletId"`
		RoundID               string `json:"roundId"`
		GameID                string `json:"gameId"`
		Kind                  string `json:"kind"`
		Money                 struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"money"`
		ReferenceExternalTransactionID string `json:"referenceExternalTransactionId"`
	} `json:"data"`
}

type Consumer struct {
	client       ReceiveDeleteAPI
	processor    InboxWagerProcessor
	queueURL     string
	consumerName string
	waitSeconds  int32
}

func NewConsumer(client ReceiveDeleteAPI, processor InboxWagerProcessor, queueURL, consumerName string) (*Consumer, error) {
	queueURL, consumerName = strings.TrimSpace(queueURL), strings.TrimSpace(consumerName)
	if client == nil || processor == nil || queueURL == "" || consumerName == "" {
		return nil, errors.New("SQS client, processor, queue URL and consumer name are required")
	}
	return &Consumer{client: client, processor: processor, queueURL: queueURL, consumerName: consumerName, waitSeconds: 20}, nil
}

// Run stops polling immediately on cancellation. A message is removed only
// after ProcessInboxWagerService returns from its committed SQL transaction.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		output, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(c.queueURL), MaxNumberOfMessages: 10, WaitTimeSeconds: c.waitSeconds,
			AttributeNames: []types.QueueAttributeName{
				types.QueueAttributeName("MessageGroupId"),
				types.QueueAttributeName("ApproximateReceiveCount"),
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("receive SQS messages: %w", err)
		}
		c.handleBatch(ctx, output.Messages)
	}
}

func (c *Consumer) handleBatch(ctx context.Context, messages []types.Message) {
	groups := map[string][]types.Message{}
	for index, message := range messages {
		group := message.Attributes[string(types.MessageSystemAttributeNameMessageGroupId)]
		if group == "" {
			group = fmt.Sprintf("message-%d", index)
		}
		groups[group] = append(groups[group], message)
	}
	var workers sync.WaitGroup
	for _, group := range groups {
		group := group
		workers.Add(1)
		go func() {
			defer workers.Done()
			for _, message := range group {
				if ctx.Err() != nil {
					return
				}
				if err := c.handle(ctx, message); err != nil {
					if ctx.Err() == nil {
						slog.Error("SQS wager message was not completed", "message_id", envelopeMessageID(message.Body), "error", err)
						if _, visibilityErr := c.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
							QueueUrl: aws.String(c.queueURL), ReceiptHandle: message.ReceiptHandle,
							VisibilityTimeout: retryVisibilityTimeout(message.Attributes["ApproximateReceiveCount"]),
						}); visibilityErr != nil {
							slog.Error("could not schedule SQS retry", "message_id", envelopeMessageID(message.Body), "error", visibilityErr)
						}
					}
					return
				}
			}
		}()
	}
	workers.Wait()
}

func retryVisibilityTimeout(receiveCount string) int32 {
	attempt, err := strconv.Atoi(receiveCount)
	if err != nil || attempt < 1 {
		attempt = 1
	}
	seconds := int32(1)
	for i := 1; i < attempt && seconds < 60; i++ {
		seconds *= 2
	}
	if seconds > 60 {
		return 60
	}
	return seconds
}

func envelopeMessageID(body *string) string {
	var envelope struct {
		MessageID string `json:"messageId"`
	}
	if body != nil {
		_ = json.Unmarshal([]byte(*body), &envelope)
	}
	return envelope.MessageID
}

func (c *Consumer) handle(ctx context.Context, message types.Message) error {
	var envelope wagerEnvelope
	if err := json.Unmarshal([]byte(aws.ToString(message.Body)), &envelope); err != nil {
		return fmt.Errorf("decode SQS wager envelope: %w", err)
	}
	if envelope.Type != "WagerTransactionRequested" {
		return fmt.Errorf("unsupported SQS message type %q", envelope.Type)
	}
	if strings.TrimSpace(envelope.MessageID) == "" {
		return errors.New("messageId is required")
	}
	money, err := domain.ParseMoney(envelope.Data.Money.Amount, envelope.Data.Money.Currency)
	if err != nil {
		return fmt.Errorf("parse SQS wager money: %w", err)
	}
	command := application.ProcessWagerCommand{
		WalletID: envelope.Data.WalletID, PlayerID: envelope.Data.PlayerID,
		ProviderID: envelope.Data.ProviderID, ExternalTransactionID: envelope.Data.ExternalTransactionID,
		IdempotencyKey: envelope.Data.IdempotencyKey, RoundID: envelope.Data.RoundID, GameID: envelope.Data.GameID,
		Kind: domain.TransactionKind(envelope.Data.Kind), Amount: money,
		ReferenceExternalTransactionID: envelope.Data.ReferenceExternalTransactionID,
		CorrelationID:                  envelope.MessageID,
	}
	if _, err := c.processor.Execute(ctx, application.InboxWagerCommand{
		ConsumerName: c.consumerName, MessageID: envelope.MessageID, Wager: command,
	}); err != nil {
		return fmt.Errorf("process SQS wager %s: %w", envelope.MessageID, err)
	}
	if _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(c.queueURL), ReceiptHandle: message.ReceiptHandle}); err != nil {
		return fmt.Errorf("delete processed SQS message %s: %w", envelope.MessageID, err)
	}
	return nil
}
