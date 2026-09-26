package application

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

// OutboxEvent é o envelope persistido para publicação posterior.
type OutboxEvent struct {
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	AggregateID   string          `json:"aggregateId"`
	CorrelationID string          `json:"correlationId,omitempty"`
	CausationID   string          `json:"causationId,omitempty"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Version       int             `json:"version"`
	Data          json.RawMessage `json:"data"`
	Attempts      int             `json:"-"`
}

type wagerProcessedData struct {
	TransactionID string     `json:"transactionId"`
	WalletID      string     `json:"walletId"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	Balance       eventMoney `json:"balance"`
}

type eventMoney struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type wagerRejectedData struct {
	TransactionID string `json:"transactionId"`
	WalletID      string `json:"walletId"`
	Kind          string `json:"kind"`
	FailureCode   string `json:"failureCode"`
}

type wagerPendingReferenceData struct {
	TransactionID string `json:"transactionId"`
	WalletID      string `json:"walletId"`
	ProviderID    string `json:"providerId"`
	ReferenceID   string `json:"referenceExternalTransactionId"`
}

type walletBalanceChangedData struct {
	WalletID      string     `json:"walletId"`
	TransactionID string     `json:"transactionId"`
	Direction     string     `json:"direction"`
	Money         eventMoney `json:"money"`
	BalanceBefore eventMoney `json:"balanceBefore"`
	BalanceAfter  eventMoney `json:"balanceAfter"`
	WalletVersion int64      `json:"walletVersion"`
}

func eventMoneyFrom(money domain.Money) eventMoney {
	amount := strings.TrimSuffix(money.String(), " "+money.Currency)
	return eventMoney{Amount: amount, Currency: money.Currency}
}
