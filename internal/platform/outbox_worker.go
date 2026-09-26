package platform

import (
	"context"
	"log/slog"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
)

type OutboxWorker struct {
	dispatcher *application.OutboxDispatcher
	logger     *slog.Logger
	interval   time.Duration
}

func NewOutboxWorker(dispatcher *application.OutboxDispatcher, logger *slog.Logger) *OutboxWorker {
	return &OutboxWorker{dispatcher: dispatcher, logger: logger, interval: time.Second}
}

// Run polls until canceled. A fresh batch is claimed only after the previous
// batch finishes, so one worker does not flood SQS or build an in-memory queue.
func (w *OutboxWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		_, err := w.dispatcher.DispatchBatch(ctx)
		if err != nil && ctx.Err() == nil {
			w.logger.Error("outbox dispatch failed", "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
