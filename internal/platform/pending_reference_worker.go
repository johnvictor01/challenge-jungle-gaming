package platform

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/observability"
)

type PendingReferenceScanner interface {
	DuePendingReferenceIDs(context.Context, int) ([]string, error)
}

type PendingReferenceResolver interface {
	Execute(context.Context, string) (application.ProcessWagerResult, error)
}

type PendingReferenceWorker struct {
	scanner   PendingReferenceScanner
	resolver  PendingReferenceResolver
	logger    *slog.Logger
	interval  time.Duration
	batchSize int
}

func NewPendingReferenceWorker(scanner PendingReferenceScanner, resolver PendingReferenceResolver, logger *slog.Logger) (*PendingReferenceWorker, error) {
	if scanner == nil || resolver == nil {
		return nil, errors.New("pending reference scanner and resolver are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PendingReferenceWorker{scanner: scanner, resolver: resolver, logger: logger, interval: time.Second, batchSize: 50}, nil
}

func (w *PendingReferenceWorker) RunOnce(ctx context.Context) error {
	ids, err := w.scanner.DuePendingReferenceIDs(ctx, w.batchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		result, err := w.resolver.Execute(ctx, id)
		if err != nil {
			observability.Default.Inc("pending_reference_worker_error")
			w.logger.Error("could not resolve pending wager reference", "transaction_id", id, "error", err)
			continue
		}
		observability.Default.Inc("pending_reference_resolution_" + string(result.Status))
	}
	return nil
}

func (w *PendingReferenceWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.logger.Error("pending reference scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
