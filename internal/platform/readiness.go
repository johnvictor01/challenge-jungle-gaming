package platform

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type DatabasePinger interface{ Ping(context.Context) error }

type QueueAttributeReader interface {
	GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
}

// DependenciesReadiness requires PostgreSQL and both configured SQS queues.
type DependenciesReadiness struct {
	Database  DatabasePinger
	SQS       QueueAttributeReader
	QueueURLs []string
}

func (r DependenciesReadiness) Ping(ctx context.Context) error {
	if r.Database == nil || r.SQS == nil || len(r.QueueURLs) == 0 {
		return errors.New("readiness dependencies are not configured")
	}
	if err := r.Database.Ping(ctx); err != nil {
		return err
	}
	for _, queueURL := range r.QueueURLs {
		if queueURL == "" {
			return errors.New("readiness queue URL is empty")
		}
		if _, err := r.SQS.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl: aws.String(queueURL), AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
		}); err != nil {
			return err
		}
	}
	return nil
}
