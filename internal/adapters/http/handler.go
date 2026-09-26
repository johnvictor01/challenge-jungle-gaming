package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/observability"
)

type WalletOpener interface {
	Execute(context.Context, application.OpenWalletCommand) (application.OpenWalletResult, error)
}

type WagerProcessor interface {
	Execute(context.Context, application.ProcessWagerCommand) (application.ProcessWagerResult, error)
}

type DataQueries interface {
	Wallet(context.Context, string) (*domain.Wallet, error)
	Transaction(context.Context, string) (*domain.WagerTransaction, error)
	ProviderTransaction(context.Context, string, string) (*domain.WagerTransaction, error)
	Ledger(context.Context, string, string, int) (application.LedgerPage, error)
	Reconcile(context.Context, string) (application.Reconciliation, error)
}

type ReadinessChecker interface{ Ping(context.Context) error }

type Handler struct {
	openWallet WalletOpener
	process    WagerProcessor
	queries    DataQueries
	auth       TokenAuthenticator
	readiness  ReadinessChecker
	logger     *slog.Logger
}

func NewHandler(openWallet WalletOpener, process WagerProcessor, queries DataQueries, auth TokenAuthenticator, readiness ReadinessChecker, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{openWallet: openWallet, process: process, queries: queries, auth: auth, readiness: readiness, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", h.live)
	mux.HandleFunc("GET /health/ready", h.ready)
	mux.Handle("GET /metrics", observability.Default)
	mux.Handle("POST /wallets", h.secure("wallet:write", http.HandlerFunc(h.createWallet)))
	mux.Handle("GET /wallets/{walletID}", h.secure("wallet:read", http.HandlerFunc(h.getWallet)))
	mux.Handle("GET /wallets/{walletID}/ledger", h.secure("wallet:read", http.HandlerFunc(h.listLedger)))
	mux.Handle("POST /wallets/{walletID}/reconciliation", h.secure("wallet:reconcile", http.HandlerFunc(h.reconcile)))
	mux.Handle("POST /wagering/transactions", h.secure("wager:write", http.HandlerFunc(h.createWager)))
	mux.Handle("GET /wagering/transactions/{transactionID}", h.secure("wager:read", http.HandlerFunc(h.getTransaction)))
	mux.Handle("GET /providers/{providerID}/wagering/transactions/{externalID}", h.secure("wager:read", http.HandlerFunc(h.getProviderTransaction)))
	return recoverMiddleware(h.logger, mux)
}

func (h *Handler) secure(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
			return
		}
		principal, err := h.auth.Authenticate(r.Context(), parts[1])
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "access token is invalid or expired")
			return
		}
		if !principal.HasRole(role) {
			writeError(w, http.StatusForbidden, "forbidden", "the authenticated client lacks the required role")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
	})
}

type principalContextKey struct{}

func principalFrom(ctx context.Context) Principal {
	principal, _ := ctx.Value(principalContextKey{}).(Principal)
	return principal
}

func (h *Handler) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if h.readiness == nil || h.readiness.Ping(r.Context()) != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "required dependencies are unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type moneyRequest struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type openWalletRequest struct {
	PlayerID       string       `json:"playerId"`
	InitialBalance moneyRequest `json:"initialBalance"`
}

func (h *Handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var request openWalletRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	initial, err := domain.ParseMoney(request.InitialBalance.Amount, request.InitialBalance.Currency)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_money", err.Error())
		return
	}
	result, err := h.openWallet.Execute(r.Context(), application.OpenWalletCommand{PlayerID: request.PlayerID, InitialBalance: initial, CorrelationID: r.Header.Get("X-Correlation-ID")})
	if err != nil {
		h.writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, walletResponseFrom(result.Wallet))
}

type wagerRequest struct {
	ProviderID                     string       `json:"providerId"`
	ExternalTransactionID          string       `json:"externalTransactionId"`
	PlayerID                       string       `json:"playerId"`
	WalletID                       string       `json:"walletId"`
	RoundID                        string       `json:"roundId"`
	GameID                         string       `json:"gameId"`
	Kind                           string       `json:"kind"`
	Money                          moneyRequest `json:"money"`
	ReferenceExternalTransactionID string       `json:"referenceExternalTransactionId"`
}

func (h *Handler) createWager(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r.Context())
	if principal.ProviderID == "" {
		writeError(w, http.StatusForbidden, "provider_identity_missing", "the token is not mapped to a provider")
		return
	}
	var request wagerRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if request.ProviderID != "" && request.ProviderID != principal.ProviderID {
		writeError(w, http.StatusForbidden, "provider_mismatch", "providerId is determined by the authenticated client")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		writeError(w, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key header is required")
		return
	}
	money, err := domain.ParseMoney(request.Money.Amount, request.Money.Currency)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_money", err.Error())
		return
	}
	result, err := h.process.Execute(r.Context(), application.ProcessWagerCommand{
		WalletID: request.WalletID, PlayerID: request.PlayerID, ProviderID: principal.ProviderID,
		ExternalTransactionID: request.ExternalTransactionID, IdempotencyKey: r.Header.Get("Idempotency-Key"),
		RoundID: request.RoundID, GameID: request.GameID, Kind: domain.TransactionKind(request.Kind), Amount: money,
		ReferenceExternalTransactionID: request.ReferenceExternalTransactionID, CorrelationID: r.Header.Get("X-Correlation-ID"),
	})
	if err != nil {
		h.writeApplicationError(w, err)
		return
	}
	status := http.StatusOK
	if result.Status == domain.TransactionPendingReference || result.Status == domain.TransactionPending {
		status = http.StatusAccepted
	} else if result.Status == domain.TransactionRejected {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, wagerResponseFrom(result))
}

func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.queries.Wallet(r.Context(), r.PathValue("walletID"))
	if err != nil {
		h.writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, walletResponseFrom(wallet))
}

func (h *Handler) listLedger(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be an integer between 1 and 100")
			return
		}
		limit = parsed
	}
	page, err := h.queries.Ledger(r.Context(), r.PathValue("walletID"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeApplicationError(w, err)
		return
	}
	entries := make([]ledgerResponse, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, ledgerResponseFrom(entry))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries, "nextCursor": page.NextCursor})
}

func (h *Handler) getTransaction(w http.ResponseWriter, r *http.Request) {
	transaction, err := h.queries.Transaction(r.Context(), r.PathValue("transactionID"))
	if err != nil {
		h.writeApplicationError(w, err)
		return
	}
	principal := principalFrom(r.Context())
	if transaction.Origin != domain.TransactionExternal || transaction.ProviderID != principal.ProviderID {
		writeError(w, http.StatusNotFound, "not_found", "transaction not found")
		return
	}
	writeJSON(w, http.StatusOK, transactionResponseFrom(transaction))
}

func (h *Handler) getProviderTransaction(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r.Context())
	providerID := r.PathValue("providerID")
	if providerID != principal.ProviderID {
		writeError(w, http.StatusNotFound, "not_found", "transaction not found")
		return
	}
	transaction, err := h.queries.ProviderTransaction(r.Context(), providerID, r.PathValue("externalID"))
	if err != nil {
		h.writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, transactionResponseFrom(transaction))
}

func (h *Handler) reconcile(w http.ResponseWriter, r *http.Request) {
	result, err := h.queries.Reconcile(r.Context(), r.PathValue("walletID"))
	if err != nil {
		h.writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reconciliationResponse{
		WalletID: result.WalletID, StoredBalance: moneyResponseFrom(result.StoredBalance),
		CalculatedBalance: moneyResponseFrom(result.CalculatedBalance), Difference: moneyResponseFrom(result.Difference),
		Consistent: result.Consistent, CheckedEntries: result.CheckedEntries,
	})
}

func (h *Handler) writeApplicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, application.ErrWalletAlreadyExists), errors.Is(err, application.ErrIdempotencyConflict), errors.Is(err, application.ErrExternalTransactionConflict), errors.Is(err, application.ErrPersistenceConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, application.ErrInvalidCursor):
		writeError(w, http.StatusBadRequest, "invalid_cursor", err.Error())
	case errors.Is(err, domain.ErrInvalidTransaction), errors.Is(err, domain.ErrInvalidTransactionAmount), errors.Is(err, domain.ErrTransactionCurrency), errors.Is(err, domain.ErrInvalidCurrency), errors.Is(err, domain.ErrInvalidPlayerID), errors.Is(err, domain.ErrNegativeBalance):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		h.logger.Error("request failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "request could not be completed")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("HTTP handler panic", "panic", recovered)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type moneyResponse struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type walletResponse struct {
	ID      string        `json:"id"`
	Player  string        `json:"playerId"`
	Balance moneyResponse `json:"balance"`
	Version int64         `json:"version"`
}

type wagerResponse struct {
	TransactionID    string         `json:"transactionId"`
	Status           string         `json:"status"`
	Balance          *moneyResponse `json:"balance,omitempty"`
	FailureCode      string         `json:"failureCode,omitempty"`
	IdempotentReplay bool           `json:"idempotentReplay"`
}

type ledgerResponse struct {
	ID            string        `json:"id"`
	TransactionID string        `json:"transactionId"`
	Direction     string        `json:"direction"`
	Money         moneyResponse `json:"money"`
	BalanceBefore moneyResponse `json:"balanceBefore"`
	BalanceAfter  moneyResponse `json:"balanceAfter"`
	CreatedAt     string        `json:"createdAt"`
}

type transactionResponse struct {
	TransactionID                  string         `json:"transactionId"`
	ProviderID                     string         `json:"providerId,omitempty"`
	ExternalTransactionID          string         `json:"externalTransactionId,omitempty"`
	PlayerID                       string         `json:"playerId"`
	WalletID                       string         `json:"walletId"`
	RoundID                        string         `json:"roundId,omitempty"`
	GameID                         string         `json:"gameId,omitempty"`
	Kind                           string         `json:"kind"`
	Status                         string         `json:"status"`
	Money                          moneyResponse  `json:"money"`
	ReferenceExternalTransactionID string         `json:"referenceExternalTransactionId,omitempty"`
	FailureCode                    string         `json:"failureCode,omitempty"`
	Balance                        *moneyResponse `json:"balance,omitempty"`
}

type reconciliationResponse struct {
	WalletID          string        `json:"walletId"`
	StoredBalance     moneyResponse `json:"storedBalance"`
	CalculatedBalance moneyResponse `json:"calculatedBalance"`
	Difference        moneyResponse `json:"difference"`
	Consistent        bool          `json:"consistent"`
	CheckedEntries    int64         `json:"checkedEntries"`
}

func moneyResponseFrom(money domain.Money) moneyResponse {
	amount := strings.TrimSuffix(money.String(), " "+money.Currency)
	return moneyResponse{Amount: amount, Currency: money.Currency}
}

func walletResponseFrom(wallet *domain.Wallet) walletResponse {
	return walletResponse{ID: wallet.ID, Player: wallet.PlayerID, Balance: moneyResponseFrom(wallet.Balance), Version: wallet.Version}
}

func wagerResponseFrom(result application.ProcessWagerResult) wagerResponse {
	response := wagerResponse{TransactionID: result.TransactionID, Status: string(result.Status), FailureCode: result.FailureCode, IdempotentReplay: result.IdempotentReplay}
	if result.Balance != nil {
		balance := moneyResponseFrom(*result.Balance)
		response.Balance = &balance
	}
	return response
}

func transactionResponseFrom(transaction *domain.WagerTransaction) transactionResponse {
	response := transactionResponse{
		TransactionID: transaction.ID, ProviderID: transaction.ProviderID, ExternalTransactionID: transaction.ExternalTransactionID,
		PlayerID: transaction.PlayerID, WalletID: transaction.WalletID, RoundID: transaction.RoundID, GameID: transaction.GameID,
		Kind: string(transaction.Kind), Status: string(transaction.Status), Money: moneyResponseFrom(transaction.Amount),
		ReferenceExternalTransactionID: transaction.ReferenceExternalTransactionID, FailureCode: transaction.FailureCode,
	}
	if transaction.ResultBalance != nil {
		balance := moneyResponseFrom(*transaction.ResultBalance)
		response.Balance = &balance
	}
	return response
}

func ledgerResponseFrom(entry *domain.WalletLedgerEntry) ledgerResponse {
	return ledgerResponse{ID: entry.ID, TransactionID: entry.TransactionID, Direction: string(entry.Direction), Money: moneyResponseFrom(entry.Amount), BalanceBefore: moneyResponseFrom(entry.BalanceBefore), BalanceAfter: moneyResponseFrom(entry.BalanceAfter), CreatedAt: entry.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
