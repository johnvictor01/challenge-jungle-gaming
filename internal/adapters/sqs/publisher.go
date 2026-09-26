package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
)

type SendMessageAPI interface {
	SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

type Publisher struct {
	client   SendMessageAPI
	queueURL string
	groupID  string
}

func NewPublisher(client SendMessageAPI, queueURL, groupID string) (*Publisher, error) {
	queueURL, groupID = strings.TrimSpace(queueURL), strings.TrimSpace(groupID)
	if client == nil || queueURL == "" {
		return nil, errors.New("SQS client and queue URL are required")
	}
	return &Publisher{client: client, queueURL: queueURL, groupID: groupID}, nil
}

func NewAWSClient(ctx context.Context, region, endpoint string) (*sqs.Client, error) {
	options := []func(*config.LoadOptions) error{}
	if strings.TrimSpace(region) != "" {
		options = append(options, config.WithRegion(strings.TrimSpace(region)))
	}
	awsConfig, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	clientOptions := []func(*sqs.Options){}
	if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
		clientOptions = append(clientOptions, func(options *sqs.Options) { options.BaseEndpoint = aws.String(endpoint) })
	}
	return sqs.NewFromConfig(awsConfig, clientOptions...), nil
}

// Publish keeps EventID as MessageDeduplicationId for FIFO queues and in the
// body for standard queues. Repeated sends therefore preserve event identity.
func (p *Publisher) Publish(ctx context.Context, event application.OutboxEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	input := &sqs.SendMessageInput{QueueUrl: aws.String(p.queueURL), MessageBody: aws.String(string(body))}
	groupID := event.AggregateID
	if groupID == "" {
		groupID = p.groupID
	}
	if groupID != "" {
		input.MessageGroupId = aws.String(groupID)
		input.MessageDeduplicationId = aws.String(event.EventID)
	} else {
		input.MessageAttributes = map[string]types.MessageAttributeValue{
			"eventId":   {DataType: aws.String("String"), StringValue: aws.String(event.EventID)},
			"eventType": {DataType: aws.String("String"), StringValue: aws.String(event.EventType)},
		}
	}
	_, err = p.client.SendMessage(ctx, input)
	return err
}
