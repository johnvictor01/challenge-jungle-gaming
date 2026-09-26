package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

type OpenWalletCommand struct {
	PlayerID       string
	InitialBalance domain.Money
	CorrelationID  string
}

type OpenWalletResult struct {
	Wallet  *domain.Wallet
	Opening *domain.WagerTransaction
}

type OpenWalletService struct {
	uow UnitOfWork
	ids IDGenerator
}

func NewOpenWalletService(uow UnitOfWork, ids IDGenerator) *OpenWalletService {
	return &OpenWalletService{uow: uow, ids: ids}
}

// Execute cria a carteira e, se houver saldo inicial positivo, também registra
// OPENING, ledger e eventos na mesma UnitOfWork.
func (s *OpenWalletService) Execute(ctx context.Context, command OpenWalletCommand) (OpenWalletResult, error) {
	if s == nil || s.uow == nil || s.ids == nil {
		return OpenWalletResult{}, errors.New("open wallet service is not configured")
	}
	var result OpenWalletResult
	err := s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		wallet, err := repositories.Wallets.FindByPlayerAndCurrency(ctx, command.PlayerID, command.InitialBalance.Currency)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if wallet != nil {
			return ErrWalletAlreadyExists
		}

		wallet, err = domain.NewWallet(command.PlayerID, command.InitialBalance.Currency, command.InitialBalance.Units)
		if err != nil {
			return err
		}
		if err := repositories.Wallets.Create(ctx, wallet); err != nil {
			if errors.Is(err, ErrPersistenceConflict) {
				return ErrWalletAlreadyExists
			}
			return err
		}
		result.Wallet = wallet
		correlationID := command.CorrelationID
		if correlationID == "" {
			correlationID = wallet.ID
		}

		if command.InitialBalance.Units == 0 {
			return nil
		}
		openingID, err := s.ids.NewID()
		if err != nil {
			return err
		}
		opening, err := domain.NewOpeningWagerTransaction(openingID, wallet, command.InitialBalance)
		if err != nil {
			return err
		}
		if err := repositories.Transactions.Create(ctx, opening); err != nil {
			return mapPersistenceConflict(err)
		}
		ledgerID, err := s.ids.NewID()
		if err != nil {
			return err
		}
		ledger, err := domain.NewWalletLedgerEntry(ledgerID, wallet.ID, opening.ID, domain.DirectionCredit,
			command.InitialBalance, domain.Money{Currency: wallet.Currency}, wallet.Balance)
		if err != nil {
			return err
		}
		if err := repositories.Ledger.Create(ctx, ledger); err != nil {
			return err
		}
		if err := s.appendOpeningEvents(ctx, repositories, opening, ledger, correlationID); err != nil {
			return err
		}
		result.Opening = opening
		return nil
	})
	if err != nil {
		return OpenWalletResult{}, err
	}
	return result, nil
}

func (s *OpenWalletService) appendOpeningEvents(ctx context.Context, repositories Repositories, opening *domain.WagerTransaction, ledger *domain.WalletLedgerEntry, correlationID string) error {
	processedPayload, err := json.Marshal(wagerProcessedData{
		TransactionID: opening.ID, WalletID: opening.WalletID, Kind: string(opening.Kind),
		Status: string(opening.Status), Balance: eventMoneyFrom(opening.Amount),
	})
	if err != nil {
		return err
	}
	if err := s.appendEvent(ctx, repositories, "WagerTransactionProcessed", opening.WalletID, opening.ID, correlationID, json.RawMessage(processedPayload)); err != nil {
		return err
	}
	changedPayload, err := json.Marshal(walletBalanceChangedData{
		WalletID: opening.WalletID, TransactionID: opening.ID, Direction: string(ledger.Direction),
		Money:         eventMoneyFrom(ledger.Amount),
		BalanceBefore: eventMoneyFrom(ledger.BalanceBefore), BalanceAfter: eventMoneyFrom(ledger.BalanceAfter), WalletVersion: 1,
	})
	if err != nil {
		return err
	}
	return s.appendEvent(ctx, repositories, "WalletBalanceChanged", opening.WalletID, opening.ID, correlationID, json.RawMessage(changedPayload))
}

func (s *OpenWalletService) appendEvent(ctx context.Context, repositories Repositories, eventType, aggregateID, causationID, correlationID string, payload json.RawMessage) error {
	eventID, err := s.ids.NewID()
	if err != nil {
		return fmt.Errorf("generate outbox event ID: %w", err)
	}
	return repositories.Outbox.Append(ctx, OutboxEvent{
		EventID: eventID, EventType: eventType, AggregateID: aggregateID,
		CorrelationID: correlationID, CausationID: causationID,
		OccurredAt: time.Now().UTC(), Version: 1, Data: payload,
	})
}
