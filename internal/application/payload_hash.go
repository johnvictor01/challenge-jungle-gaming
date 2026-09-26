package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// canonicalWagerPayload define ordem e nomes estáveis para o hash do negócio.
// Chave de idempotência e metadados de transporte ficam fora do conteúdo.
type canonicalWagerPayload struct {
	ProviderID            string         `json:"providerId"`
	ExternalTransactionID string         `json:"externalTransactionId"`
	PlayerID              string         `json:"playerId"`
	WalletID              string         `json:"walletId"`
	RoundID               string         `json:"roundId"`
	GameID                string         `json:"gameId"`
	Kind                  string         `json:"kind"`
	Money                 canonicalMoney `json:"money"`
	ReferenceExternalID   string         `json:"referenceExternalTransactionId,omitempty"`
}

type canonicalMoney struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// CanonicalPayloadHash calcula SHA-256 do conjunto estável de campos do negócio.
// HTTP e SQS devem chamar esta mesma função depois de normalizar a entrada.
func CanonicalPayloadHash(command ProcessWagerCommand) (string, error) {
	amount := strings.TrimSuffix(command.Amount.String(), " "+command.Amount.Currency())
	payload := canonicalWagerPayload{
		ProviderID:            command.ProviderID,
		ExternalTransactionID: command.ExternalTransactionID,
		PlayerID:              command.PlayerID,
		WalletID:              command.WalletID,
		RoundID:               command.RoundID,
		GameID:                command.GameID,
		Kind:                  string(command.Kind),
		Money:                 canonicalMoney{Amount: amount, Currency: command.Amount.Currency()},
		ReferenceExternalID:   command.ReferenceExternalTransactionID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}
