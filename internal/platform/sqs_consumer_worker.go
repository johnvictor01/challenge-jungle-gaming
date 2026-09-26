package platform

import (
	"context"
	"log/slog"
	"time"
)

type SQSConsumer interface{ Run(context.Context) error }

type SQSConsumerWorker struct {
	consumer SQSConsumer
	logger   *slog.Logger
}

func NewSQSConsumerWorker(consumer SQSConsumer, logger *slog.Logger) *SQSConsumerWorker {
	return &SQSConsumerWorker{consumer: consumer, logger: logger}
}

func (w *SQSConsumerWorker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := w.consumer.Run(ctx); err != nil && ctx.Err() == nil {
			w.logger.Error("SQS consumer stopped; retrying", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}
