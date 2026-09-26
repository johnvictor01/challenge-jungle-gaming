package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/postgres"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
)

func TestKeycloakClientCredentialsToken(t *testing.T) {
	issuer := os.Getenv("TEST_OIDC_ISSUER_URL")
	secret := os.Getenv("TEST_PROVIDER_CLIENT_SECRET")
	if issuer == "" || secret == "" {
		t.Skip("set TEST_OIDC_ISSUER_URL and TEST_PROVIDER_CLIENT_SECRET for Keycloak integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tokenEndpoint := strings.TrimRight(issuer, "/") + "/protocol/openid-connect/token"
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {"provider-a"}, "client_secret": {secret}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request client credentials token: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("token endpoint status = %d", response.StatusCode)
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&tokenResponse); err != nil {
		t.Fatal(err)
	}
	if tokenResponse.AccessToken == "" {
		t.Fatal("Keycloak did not return an access token")
	}
	authenticator, err := NewOIDCAuthenticator(ctx, issuer, "wager-api")
	if err != nil {
		t.Fatalf("create OIDC authenticator: %v", err)
	}
	principal, err := authenticator.Authenticate(ctx, tokenResponse.AccessToken)
	if err != nil {
		t.Fatalf("verify Keycloak access token: %v", err)
	}
	if principal.ProviderID != "provider-a" || !principal.HasRole("wager:write") || !principal.HasRole("wager:read") {
		t.Fatalf("unexpected provider principal: %+v", principal)
	}
	if _, err := authenticator.Authenticate(ctx, tokenResponse.AccessToken+"invalid-signature"); err == nil {
		t.Fatal("token with an altered signature was accepted")
	}
}

func TestKeycloakHTTPPostgresWalletAndWagerFlow(t *testing.T) {
	issuer := os.Getenv("TEST_OIDC_ISSUER_URL")
	providerSecret := os.Getenv("TEST_PROVIDER_CLIENT_SECRET")
	internalSecret := os.Getenv("TEST_INTERNAL_CLIENT_SECRET")
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if issuer == "" || providerSecret == "" || internalSecret == "" || databaseURL == "" {
		t.Skip("set Keycloak and PostgreSQL test environment variables for end-to-end flow")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	defer store.Close()
	auth, err := NewOIDCAuthenticator(ctx, issuer, "wager-api")
	if err != nil {
		t.Fatalf("create OIDC verifier: %v", err)
	}
	internalToken := getClientToken(t, ctx, issuer, "internal-service", internalSecret)
	providerToken := getClientToken(t, ctx, issuer, "provider-a", providerSecret)
	ids := application.UUIDGenerator{}
	handler := NewHandler(application.NewOpenWalletService(store, ids), application.NewProcessWagerService(store, ids), application.NewQueryService(store), auth, store, slog.New(slog.NewTextHandler(io.Discard, nil)))

	playerID := fmt.Sprintf("http-e2e-player-%d", time.Now().UnixNano())
	openBody := `{"playerId":"` + playerID + `","initialBalance":{"amount":"1000.00","currency":"BRL"}}`
	openRequest := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(openBody))
	openRequest.Header.Set("Authorization", "Bearer "+internalToken)
	openResponse := httptest.NewRecorder()
	handler.ServeHTTP(openResponse, openRequest)
	if openResponse.Code != http.StatusCreated {
		t.Fatalf("open wallet status=%d body=%s", openResponse.Code, openResponse.Body.String())
	}
	var wallet struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(openResponse.Body.Bytes(), &wallet); err != nil || wallet.ID == "" {
		t.Fatalf("decode wallet response: wallet=%+v error=%v", wallet, err)
	}

	operationID := fmt.Sprintf("http-e2e-bet-%d", time.Now().UnixNano())
	wagerBody := `{"externalTransactionId":"` + operationID + `","playerId":"` + playerID + `","walletId":"` + wallet.ID + `","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"25.00","currency":"BRL"}}`
	wagerRequest := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(wagerBody))
	wagerRequest.Header.Set("Authorization", "Bearer "+providerToken)
	wagerRequest.Header.Set("Idempotency-Key", "http-e2e-idem-"+operationID)
	wagerResponse := httptest.NewRecorder()
	handler.ServeHTTP(wagerResponse, wagerRequest)
	if wagerResponse.Code != http.StatusOK {
		t.Fatalf("wager status=%d body=%s", wagerResponse.Code, wagerResponse.Body.String())
	}
	var result struct {
		Status  string `json:"status"`
		Balance struct {
			Amount string `json:"amount"`
		} `json:"balance"`
	}
	if err := json.Unmarshal(wagerResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "PROCESSED" || result.Balance.Amount != "975.00" {
		t.Fatalf("unexpected wager response: %+v", result)
	}
	var operation struct {
		TransactionID string `json:"transactionId"`
	}
	if err := json.Unmarshal(wagerResponse.Body.Bytes(), &operation); err != nil || operation.TransactionID == "" {
		t.Fatalf("decode operation response: operation=%+v error=%v", operation, err)
	}

	readRequest := httptest.NewRequest(http.MethodGet, "/wallets/"+wallet.ID, nil)
	readRequest.Header.Set("Authorization", "Bearer "+internalToken)
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusOK || !strings.Contains(readResponse.Body.String(), `"amount":"975.00"`) {
		t.Fatalf("wallet read status=%d body=%s", readResponse.Code, readResponse.Body.String())
	}
	ledgerRequest := httptest.NewRequest(http.MethodGet, "/wallets/"+wallet.ID+"/ledger?limit=1", nil)
	ledgerRequest.Header.Set("Authorization", "Bearer "+internalToken)
	ledgerResponse := httptest.NewRecorder()
	handler.ServeHTTP(ledgerResponse, ledgerRequest)
	if ledgerResponse.Code != http.StatusOK || !strings.Contains(ledgerResponse.Body.String(), `"nextCursor"`) {
		t.Fatalf("ledger read status=%d body=%s", ledgerResponse.Code, ledgerResponse.Body.String())
	}
	reconcileRequest := httptest.NewRequest(http.MethodPost, "/wallets/"+wallet.ID+"/reconciliation", nil)
	reconcileRequest.Header.Set("Authorization", "Bearer "+internalToken)
	reconcileResponse := httptest.NewRecorder()
	handler.ServeHTTP(reconcileResponse, reconcileRequest)
	if reconcileResponse.Code != http.StatusOK || !strings.Contains(reconcileResponse.Body.String(), `"consistent":true`) {
		t.Fatalf("reconciliation status=%d body=%s", reconcileResponse.Code, reconcileResponse.Body.String())
	}
	transactionRequest := httptest.NewRequest(http.MethodGet, "/wagering/transactions/"+operation.TransactionID, nil)
	transactionRequest.Header.Set("Authorization", "Bearer "+providerToken)
	transactionResponse := httptest.NewRecorder()
	handler.ServeHTTP(transactionResponse, transactionRequest)
	if transactionResponse.Code != http.StatusOK || !strings.Contains(transactionResponse.Body.String(), `"status":"PROCESSED"`) {
		t.Fatalf("transaction read status=%d body=%s", transactionResponse.Code, transactionResponse.Body.String())
	}
}

func getClientToken(t *testing.T, ctx context.Context, issuer, clientID, secret string) string {
	t.Helper()
	endpoint := strings.TrimRight(issuer, "/") + "/protocol/openid-connect/token"
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {clientID}, "client_secret": {secret}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request client credentials for %s: %v", clientID, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("token endpoint for %s returned %d", clientID, response.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		t.Fatal(err)
	}
	if token.AccessToken == "" {
		t.Fatalf("token endpoint returned no token for %s", clientID)
	}
	return token.AccessToken
}
