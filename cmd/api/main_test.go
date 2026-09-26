package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"go.uber.org/fx"
)

func TestApplicationGraphIsValid(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := fx.ValidateApp(options(logger)...); err != nil {
		t.Fatalf("Fx application graph is invalid: %v", err)
	}
}

// TestApplicationStartsAndStopsWorkers exercises the real Fx lifecycle with
// PostgreSQL, Keycloak discovery and LocalStack when integration env is set.
func TestApplicationStartsAndStopsWorkers(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":        os.Getenv("TEST_DATABASE_URL"),
		"OIDC_ISSUER_URL":     os.Getenv("TEST_OIDC_ISSUER_URL"),
		"OIDC_AUDIENCE":       "wager-api",
		"SQS_ENDPOINT":        os.Getenv("TEST_SQS_ENDPOINT"),
		"AWS_REGION":          "us-east-1",
		"SQS_INPUT_QUEUE_URL": os.Getenv("TEST_SQS_INPUT_QUEUE_URL"),
		"SQS_QUEUE_URL":       os.Getenv("TEST_SQS_QUEUE_URL"),
		"HTTP_ADDR":           "127.0.0.1:0",
	}
	for key, value := range values {
		if value == "" {
			t.Skip("set TEST_DATABASE_URL, TEST_OIDC_ISSUER_URL, TEST_SQS_ENDPOINT, TEST_SQS_INPUT_QUEUE_URL and TEST_SQS_QUEUE_URL to exercise the Fx lifecycle")
		}
		t.Setenv(key, value)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := fx.New(options(logger)...)
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("start Fx application: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("stop Fx application and workers: %v", err)
	}
}
