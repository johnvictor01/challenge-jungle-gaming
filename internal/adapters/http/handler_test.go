package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type staticAuthenticator struct{ principal Principal }

func (a staticAuthenticator) Authenticate(_ context.Context, token string) (Principal, error) {
	if token != "valid" {
		return Principal{}, ErrUnauthorized
	}
	return a.principal, nil
}

func testPrincipal(providerID string, roles ...string) Principal {
	roleMap := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		roleMap[role] = struct{}{}
	}
	return Principal{Subject: "service-account-provider", ClientID: "provider-client", ProviderID: providerID, Roles: roleMap}
}

type fakeOpener struct{}

func (fakeOpener) Execute(context.Context, application.OpenWalletCommand) (application.OpenWalletResult, error) {
	return application.OpenWalletResult{}, nil
}

type fakeProcessor struct {
	command application.ProcessWagerCommand
	called  bool
}

func (p *fakeProcessor) Execute(_ context.Context, command application.ProcessWagerCommand) (application.ProcessWagerResult, error) {
	p.called = true
	p.command = command
	return application.ProcessWagerResult{TransactionID: "tx-1", Status: domain.TransactionProcessed, Balance: &domain.Money{Units: 4_000, Currency: "BRL"}}, nil
}

type fakeQueries struct{}

func (fakeQueries) Wallet(context.Context, string) (*domain.Wallet, error) {
	return nil, application.ErrNotFound
}
func (fakeQueries) Transaction(context.Context, string) (*domain.WagerTransaction, error) {
	return nil, application.ErrNotFound
}
func (fakeQueries) ProviderTransaction(context.Context, string, string) (*domain.WagerTransaction, error) {
	return nil, application.ErrNotFound
}
func (fakeQueries) Ledger(context.Context, string, string, int) (application.LedgerPage, error) {
	return application.LedgerPage{}, nil
}
func (fakeQueries) Reconcile(context.Context, string) (application.Reconciliation, error) {
	return application.Reconciliation{}, nil
}

func testHandler(principal Principal, processor *fakeProcessor) http.Handler {
	return NewHandler(fakeOpener{}, processor, fakeQueries{}, staticAuthenticator{principal: principal}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHandlerRequiresValidTokenAndRole(t *testing.T) {
	processor := &fakeProcessor{}
	handler := testHandler(testPrincipal("provider-a", "wager:write"), processor)
	request := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer valid")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestMetricsEndpointIsPublic(t *testing.T) {
	handler := testHandler(testPrincipal("provider-a", "wager:write"), &fakeProcessor{})
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics status/content-type=%d/%q", response.Code, response.Header().Get("Content-Type"))
	}
}

func TestHandlerRejectsSpoofedProviderID(t *testing.T) {
	processor := &fakeProcessor{}
	handler := testHandler(testPrincipal("provider-a", "wager:write"), processor)
	body := `{"providerId":"provider-b","externalTransactionId":"bet-1","playerId":"player-1","walletId":"wallet-1","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"10.00","currency":"BRL"}}`
	request := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Idempotency-Key", "provider-a:bet-1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || processor.called {
		t.Fatalf("status/called = %d/%v, want 403/false", recorder.Code, processor.called)
	}
}

func TestHandlerTakesProviderIDFromVerifiedPrincipal(t *testing.T) {
	processor := &fakeProcessor{}
	handler := testHandler(testPrincipal("provider-a", "wager:write"), processor)
	body := `{"providerId":"provider-a","externalTransactionId":"bet-1","playerId":"player-1","walletId":"wallet-1","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"10.00","currency":"BRL"}}`
	request := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Idempotency-Key", "provider-a:bet-1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !processor.called || processor.command.ProviderID != "provider-a" {
		t.Fatalf("status/called/provider = %d/%v/%q", recorder.Code, processor.called, processor.command.ProviderID)
	}
}
