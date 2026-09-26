package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.uber.org/fx"

	httpadapter "github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/http"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/postgres"
	sqsadapter "github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/sqs"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/platform"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	fx.New(options(logger)...).Run()
}

func options(logger *slog.Logger) []fx.Option {
	return []fx.Option{
		fx.Supply(logger, application.UUIDGenerator{}),
		fx.Provide(platform.LoadConfig),
		fx.Provide(func(generator application.UUIDGenerator) application.IDGenerator { return generator }),
		fx.Provide(newPostgresStore),
		fx.Provide(func(store *postgres.Store) application.UnitOfWork { return store }),
		fx.Provide(application.NewOpenWalletService, application.NewProcessWagerService, application.NewQueryService),
		fx.Provide(func(store *postgres.Store, ids application.IDGenerator) *application.ResolvePendingReferenceService {
			return application.NewResolvePendingReferenceService(store, ids, application.ReferenceRetryPolicy{})
		}),
		fx.Provide(func(store *postgres.Store) platform.PendingReferenceScanner { return store }),
		fx.Provide(func(resolver *application.ResolvePendingReferenceService) platform.PendingReferenceResolver {
			return resolver
		}),
		fx.Provide(func(service *application.OpenWalletService) httpadapter.WalletOpener { return service }),
		fx.Provide(func(service *application.ProcessWagerService) httpadapter.WagerProcessor { return service }),
		fx.Provide(func(service *application.QueryService) httpadapter.DataQueries { return service }),
		fx.Provide(newOIDCAuthenticator),
		fx.Provide(newSQSClient, newSQSPublisher),
		fx.Provide(func(store *postgres.Store) *postgres.OutboxDispatcherRepository {
			return postgres.NewOutboxDispatcherRepository(store)
		}),
		fx.Provide(func(repository *postgres.OutboxDispatcherRepository, publisher *sqsadapter.Publisher, config platform.Config, logger *slog.Logger) (*application.OutboxDispatcher, error) {
			return application.NewOutboxDispatcher(repository, publisher, application.OutboxDispatchConfig{Owner: config.WorkerID, Logger: logger})
		}),
		fx.Provide(func(store *postgres.Store, processor *application.ProcessWagerService, client *sqs.Client, config platform.Config) (*sqsadapter.Consumer, error) {
			if config.SQSInputQueueURL == "" {
				return nil, errors.New("SQS_INPUT_QUEUE_URL is required")
			}
			inboxProcessor := application.NewProcessInboxWagerService(store, processor)
			return sqsadapter.NewConsumer(client, inboxProcessor, config.SQSInputQueueURL, "wager-api")
		}),
		fx.Provide(func(consumer *sqsadapter.Consumer) platform.SQSConsumer { return consumer }),
		fx.Provide(platform.NewOutboxWorker),
		fx.Provide(platform.NewPendingReferenceWorker),
		fx.Provide(platform.NewSQSConsumerWorker),
		fx.Provide(func(auth *httpadapter.OIDCAuthenticator) httpadapter.TokenAuthenticator { return auth }),
		fx.Provide(func(store *postgres.Store, client *sqs.Client, config platform.Config) httpadapter.ReadinessChecker {
			return platform.DependenciesReadiness{Database: store, SQS: client, QueueURLs: []string{config.SQSInputQueueURL, config.SQSQueueURL}}
		}),
		fx.Provide(httpadapter.NewHandler),
		fx.Invoke(registerHTTPServer),
		fx.Invoke(registerOutboxWorker),
		fx.Invoke(registerPendingReferenceWorker),
		fx.Invoke(registerSQSConsumerWorker),
	}
}

func registerPendingReferenceWorker(lifecycle fx.Lifecycle, worker *platform.PendingReferenceWorker, logger *slog.Logger) {
	var cancel context.CancelFunc
	var done chan struct{}
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ctx, stop := context.WithCancel(context.Background())
			cancel, done = stop, make(chan struct{})
			go func() { defer close(done); worker.Run(ctx) }()
			logger.Info("pending reference worker started")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			if done == nil {
				return nil
			}
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
}

func registerSQSConsumerWorker(lifecycle fx.Lifecycle, worker *platform.SQSConsumerWorker, logger *slog.Logger) {
	var cancel context.CancelFunc
	var done chan struct{}
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ctx, stop := context.WithCancel(context.Background())
			cancel, done = stop, make(chan struct{})
			go func() { defer close(done); worker.Run(ctx) }()
			logger.Info("SQS consumer started")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			if done == nil {
				return nil
			}
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
}

func newSQSClient(config platform.Config) (*sqs.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return sqsadapter.NewAWSClient(ctx, config.SQSRegion, config.SQSEndpoint)
}

func newSQSPublisher(client *sqs.Client, config platform.Config) (*sqsadapter.Publisher, error) {
	if config.SQSQueueURL == "" {
		return nil, errors.New("SQS_QUEUE_URL is required")
	}
	return sqsadapter.NewPublisher(client, config.SQSQueueURL, config.SQSGroupID)
}

func registerOutboxWorker(lifecycle fx.Lifecycle, worker *platform.OutboxWorker, logger *slog.Logger) {
	var cancel context.CancelFunc
	var done chan struct{}
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ctx, stop := context.WithCancel(context.Background())
			cancel, done = stop, make(chan struct{})
			go func() { defer close(done); worker.Run(ctx) }()
			logger.Info("outbox worker started")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			if done == nil {
				return nil
			}
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
}

func newPostgresStore(lifecycle fx.Lifecycle, config platform.Config) (*postgres.Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := postgres.NewPool(ctx, config.DatabaseURL)
	if err != nil {
		return nil, err
	}
	lifecycle.Append(fx.Hook{OnStop: func(context.Context) error { store.Close(); return nil }})
	return store, nil
}

func newOIDCAuthenticator(config platform.Config) (*httpadapter.OIDCAuthenticator, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpadapter.NewOIDCAuthenticator(ctx, config.OIDCIssuerURL, config.OIDCAudience)
}

func registerHTTPServer(lifecycle fx.Lifecycle, config platform.Config, handler http.Handler, logger *slog.Logger) {
	server := &http.Server{
		Addr: config.HTTPAddress, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
	}
	var listener net.Listener
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			opened, err := net.Listen("tcp", server.Addr)
			if err != nil {
				return err
			}
			listener = opened
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.Error("HTTP server stopped unexpectedly", "error", err)
				}
			}()
			logger.Info("HTTP server started", "address", server.Addr)
			return nil
		},
		OnStop: func(ctx context.Context) error { return server.Shutdown(ctx) },
	})
}
