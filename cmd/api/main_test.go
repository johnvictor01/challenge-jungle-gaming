package main

import (
	"io"
	"log/slog"
	"testing"

	"go.uber.org/fx"
)

func TestApplicationGraphIsValid(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := fx.ValidateApp(options(logger)...); err != nil {
		t.Fatalf("Fx application graph is invalid: %v", err)
	}
}
