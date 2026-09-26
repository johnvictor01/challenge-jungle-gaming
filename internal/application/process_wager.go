package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/domain"
)

// ProcessWagerCommand contém os dados já normalizados pela entrada HTTP ou SQS.
type ProcessWagerCommand struct {
	// TransactionID é interno; normalmente será gerado pelo serviço.
	TransactionID                  string
	WalletID                       string
	PlayerID                       string
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	RoundID                        string
	GameID                         string
	Kind                           domain.TransactionKind
	Amount                         domain.Money
	ReferenceExternalTransactionID string
	CorrelationID                  string
}

// ProcessWagerResult representa o resultado persistido devolvido à entrada.
type ProcessWagerResult struct {
	TransactionID    string
	Status           domain.TransactionStatus
	Balance          *domain.Money
	FailureCode      string
	IdempotentReplay bool
}

type ProcessWagerService struct {
	uow UnitOfWork
	ids IDGenerator
}

func NewProcessWagerService(uow UnitOfWork, ids IDGenerator) *ProcessWagerService {
	return &ProcessWagerService{uow: uow, ids: ids}
}

// Execute aplica uma operação e grava seus efeitos dentro de uma única UnitOfWork.
func (s *ProcessWagerService) Execute(ctx context.Context, command ProcessWagerCommand) (ProcessWagerResult, error) {
	if s == nil || s.uow == nil || s.ids == nil {
		return ProcessWagerResult{}, errors.New("process wager service is not configured")
	}
	hash, err := CanonicalPayloadHash(command)
	if err != nil {
		return ProcessWagerResult{}, err
	}
	if command.PayloadHash != "" && command.PayloadHash != hash {
		return ProcessWagerResult{}, ErrPayloadHashMismatch
	}
	command.PayloadHash = hash
	var result ProcessWagerResult
	err = s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		var err error
		result, err = s.executeInTransaction(ctx, repositories, command)
		return err
	})
	if errors.Is(err, ErrPersistenceConflict) {
		// Uma requisição concorrente pode ter gravado a mesma chave após nossa
		// primeira leitura. Consultamos novamente fora da transação abortada.
		return s.resolvePersistenceConflict(ctx, command)
	}
	if err != nil {
		return ProcessWagerResult{}, err
	}
	return result, nil
}

func (s *ProcessWagerService) resolvePersistenceConflict(ctx context.Context, command ProcessWagerCommand) (ProcessWagerResult, error) {
	var result ProcessWagerResult
	err := s.uow.WithinTransaction(ctx, func(repositories Repositories) error {
		found, err := repositories.Transactions.FindByIdempotencyKey(ctx, command.ProviderID, command.IdempotencyKey)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if found != nil {
			if found.PayloadHash != command.PayloadHash {
				return ErrIdempotencyConflict
			}
			result = resultFromTransaction(found, true)
			return nil
		}
		found, err = repositories.Transactions.FindByExternalID(ctx, command.ProviderID, command.ExternalTransactionID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if found != nil {
			return ErrExternalTransactionConflict
		}
		return ErrPersistenceConflict
	})
	if err != nil {
		return ProcessWagerResult{}, err
	}
	return result, nil
}

func (s *ProcessWagerService) executeInTransaction(ctx context.Context, repositories Repositories, command ProcessWagerCommand) (ProcessWagerResult, error) {
	if strings.TrimSpace(command.ProviderID) == "" || strings.TrimSpace(command.ExternalTransactionID) == "" || strings.TrimSpace(command.IdempotencyKey) == "" ||
		command.WalletID == "" || command.PlayerID == "" || command.RoundID == "" || command.GameID == "" {
		return ProcessWagerResult{}, fmt.Errorf("%w: external IDs, wallet, player, round and game are required", domain.ErrInvalidTransaction)
	}

	// Primeiro verifica replay: a carteira pode já ter recebido outras operações.
	existing, err := repositories.Transactions.FindByIdempotencyKey(ctx, command.ProviderID, command.IdempotencyKey)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ProcessWagerResult{}, err
	}
	if existing != nil {
		if existing.PayloadHash != command.PayloadHash {
			return ProcessWagerResult{}, ErrIdempotencyConflict
		}
		return resultFromTransaction(existing, true), nil
	}

	// A mesma transação externa não pode ser reaplicada com outra chave.
	existing, err = repositories.Transactions.FindByExternalID(ctx, command.ProviderID, command.ExternalTransactionID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ProcessWagerResult{}, err
	}
	if existing != nil {
		return ProcessWagerResult{}, ErrExternalTransactionConflict
	}
	// A restrição única do banco é a defesa final contra duas requisições simultâneas.

	wallet, err := repositories.Wallets.FindByID(ctx, command.WalletID)
	if err != nil {
		return ProcessWagerResult{}, err
	}
	if wallet == nil || wallet.PlayerID != command.PlayerID {
		return ProcessWagerResult{}, ErrNotFound
	}
	transactionID := command.TransactionID
	if transactionID == "" {
		transactionID, err = s.ids.NewID()
		if err != nil {
			return ProcessWagerResult{}, err
		}
	}
	transaction, err := domain.NewExternalWagerTransaction(domain.ExternalWagerInput{
		ID: transactionID, Wallet: wallet, ProviderID: command.ProviderID,
		ExternalTransactionID: command.ExternalTransactionID, IdempotencyKey: command.IdempotencyKey,
		PayloadHash: command.PayloadHash, Kind: command.Kind, Amount: command.Amount,
		RoundID: command.RoundID, GameID: command.GameID,
		ReferenceExternalTransactionID: command.ReferenceExternalTransactionID,
	})
	if err != nil {
		return ProcessWagerResult{}, err
	}
	correlationID := command.CorrelationID
	if correlationID == "" {
		correlationID = transaction.ID
	}

	var reference *domain.WagerTransaction
	if transaction.ReferenceExternalTransactionID != "" {
		reference, err = repositories.Transactions.FindByExternalID(ctx, command.ProviderID, transaction.ReferenceExternalTransactionID)
		if errors.Is(err, ErrNotFound) || (err == nil && (reference == nil || reference.Status == domain.TransactionPending || reference.Status == domain.TransactionPendingReference)) {
			if err := transaction.WaitForReference(); err != nil {
				return ProcessWagerResult{}, err
			}
			if err := repositories.Transactions.Create(ctx, transaction); err != nil {
				return ProcessWagerResult{}, err
			}
			if err := s.appendPendingReferenceEvent(ctx, repositories, transaction, correlationID); err != nil {
				return ProcessWagerResult{}, err
			}
			return resultFromTransaction(transaction, false), nil
		}
		if err != nil {
			return ProcessWagerResult{}, err
		}
		if reference != nil {
			if reference.Status == domain.TransactionRejected || reference.Status == domain.TransactionFailed {
				return s.reject(ctx, repositories, transaction, "REFERENCE_NOT_PROCESSED", correlationID)
			}
			if err := transaction.WaitForReference(); err != nil {
				return ProcessWagerResult{}, err
			}
			if err := transaction.ResumeAfterReference(*reference); err != nil {
				if errors.Is(err, domain.ErrInvalidReference) {
					return s.reject(ctx, repositories, transaction, "INVALID_REFERENCE", correlationID)
				}
				return ProcessWagerResult{}, err
			}
			if transaction.Kind == domain.TransactionRefund || transaction.Kind == domain.TransactionRollback {
				reversal, err := repositories.Transactions.FindSuccessfulReversal(ctx, reference.ID)
				if err != nil && !errors.Is(err, ErrNotFound) {
					return ProcessWagerResult{}, err
				}
				if reversal != nil {
					return s.reject(ctx, repositories, transaction, "REFERENCE_ALREADY_REVERSED", correlationID)
				}
			}
		}
	}
	// Referência terminal não processada é rejeitada com código estável; referências
	// ausentes ou ainda pendentes aguardam o worker de resolução.

	before := wallet.Balance
	oldVersion := wallet.Version
	var pendingLedgerEntry *domain.WalletLedgerEntry
	if err := applyWalletOperation(wallet, transaction, reference); err != nil {
		if errors.Is(err, domain.ErrInsufficientFunds) {
			failureCode := "INSUFFICIENT_FUNDS"
			if transaction.Kind == domain.TransactionRollback {
				failureCode = "REVERSAL_INSUFFICIENT_FUNDS"
			}
			return s.reject(ctx, repositories, transaction, failureCode, correlationID)
		}
		return ProcessWagerResult{}, err
	}

	if wallet.Version != oldVersion {
		if err := repositories.Wallets.Update(ctx, wallet, oldVersion); err != nil {
			return ProcessWagerResult{}, err
		}
		ledgerID, err := s.ids.NewID()
		if err != nil {
			return ProcessWagerResult{}, err
		}
		direction := domain.DirectionCredit
		if wallet.Balance.Units < before.Units {
			direction = domain.DirectionDebit
		}
		entry, err := domain.NewWalletLedgerEntry(ledgerID, wallet.ID, transaction.ID, direction, transaction.Amount, before, wallet.Balance)
		if err != nil {
			return ProcessWagerResult{}, err
		}
		pendingLedgerEntry = entry
	}

	if err := transaction.MarkProcessed(wallet.Balance); err != nil {
		return ProcessWagerResult{}, err
	}
	if err := repositories.Transactions.Create(ctx, transaction); err != nil {
		return ProcessWagerResult{}, mapPersistenceConflict(err)
	}
	if pendingLedgerEntry != nil {
		if err := repositories.Ledger.Create(ctx, pendingLedgerEntry); err != nil {
			return ProcessWagerResult{}, err
		}
	}
	if err := s.appendProcessedEvent(ctx, repositories, transaction, correlationID); err != nil {
		return ProcessWagerResult{}, err
	}
	if wallet.Version != oldVersion {
		if err := s.appendBalanceChangedEvent(ctx, repositories, transaction, before, wallet, correlationID); err != nil {
			return ProcessWagerResult{}, err
		}
	}
	return resultFromTransaction(transaction, false), nil
}

func applyWalletOperation(wallet *domain.Wallet, transaction *domain.WagerTransaction, reference *domain.WagerTransaction) error {
	switch transaction.Kind {
	case domain.TransactionBet:
		return wallet.Debit(transaction.Amount)
	case domain.TransactionWin, domain.TransactionRefund:
		return wallet.Credit(transaction.Amount)
	case domain.TransactionLoss:
		return nil
	case domain.TransactionRollback:
		if reference == nil {
			return domain.ErrInvalidReference
		}
		if reference.Kind == domain.TransactionBet {
			return wallet.Credit(transaction.Amount)
		}
		return wallet.Debit(transaction.Amount)
	default:
		return domain.ErrInvalidTransaction
	}
}

func (s *ProcessWagerService) reject(ctx context.Context, repositories Repositories, transaction *domain.WagerTransaction, code, correlationID string) (ProcessWagerResult, error) {
	if err := transaction.Reject(code); err != nil {
		return ProcessWagerResult{}, err
	}
	if err := repositories.Transactions.Create(ctx, transaction); err != nil {
		return ProcessWagerResult{}, mapPersistenceConflict(err)
	}
	if err := s.appendRejectedEvent(ctx, repositories, transaction, correlationID); err != nil {
		return ProcessWagerResult{}, err
	}
	return resultFromTransaction(transaction, false), nil
}

func resultFromTransaction(transaction *domain.WagerTransaction, replay bool) ProcessWagerResult {
	var balance *domain.Money
	if transaction.ResultBalance != nil {
		copy := *transaction.ResultBalance
		balance = &copy
	}
	return ProcessWagerResult{
		TransactionID: transaction.ID, Status: transaction.Status, Balance: balance,
		FailureCode: transaction.FailureCode, IdempotentReplay: replay,
	}
}

func mapPersistenceConflict(err error) error {
	if errors.Is(err, ErrPersistenceConflict) {
		return ErrPersistenceConflict
	}
	return err
}

func (s *ProcessWagerService) appendPendingReferenceEvent(ctx context.Context, repositories Repositories, transaction *domain.WagerTransaction, correlationID string) error {
	return s.appendEvent(ctx, repositories, "WagerTransactionPendingReference", transaction.WalletID, transaction.ID, correlationID,
		wagerPendingReferenceData{TransactionID: transaction.ID, WalletID: transaction.WalletID, ProviderID: transaction.ProviderID, ReferenceID: transaction.ReferenceExternalTransactionID})
}

func (s *ProcessWagerService) appendRejectedEvent(ctx context.Context, repositories Repositories, transaction *domain.WagerTransaction, correlationID string) error {
	return s.appendEvent(ctx, repositories, "WagerTransactionRejected", transaction.WalletID, transaction.ID, correlationID,
		wagerRejectedData{TransactionID: transaction.ID, WalletID: transaction.WalletID, Kind: string(transaction.Kind), FailureCode: transaction.FailureCode})
}

func (s *ProcessWagerService) appendProcessedEvent(ctx context.Context, repositories Repositories, transaction *domain.WagerTransaction, correlationID string) error {
	balance := eventMoney{Currency: transaction.Currency}
	if transaction.ResultBalance != nil {
		balance = eventMoneyFrom(*transaction.ResultBalance)
	}
	return s.appendEvent(ctx, repositories, "WagerTransactionProcessed", transaction.WalletID, transaction.ID, correlationID,
		wagerProcessedData{TransactionID: transaction.ID, WalletID: transaction.WalletID, Kind: string(transaction.Kind), Status: string(transaction.Status), Balance: balance})
}

func (s *ProcessWagerService) appendBalanceChangedEvent(ctx context.Context, repositories Repositories, transaction *domain.WagerTransaction, before domain.Money, wallet *domain.Wallet, correlationID string) error {
	direction := string(domain.DirectionCredit)
	if wallet.Balance.Units < before.Units {
		direction = string(domain.DirectionDebit)
	}
	data := walletBalanceChangedData{
		WalletID: wallet.ID, TransactionID: transaction.ID, Direction: direction,
		Money:         eventMoneyFrom(transaction.Amount),
		BalanceBefore: eventMoneyFrom(before), BalanceAfter: eventMoneyFrom(wallet.Balance), WalletVersion: wallet.Version,
	}
	return s.appendEvent(ctx, repositories, "WalletBalanceChanged", wallet.ID, transaction.ID, correlationID, data)
}

func (s *ProcessWagerService) appendEvent(ctx context.Context, repositories Repositories, eventType, aggregateID, causationID, correlationID string, data any) error {
	eventID, err := s.ids.NewID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return repositories.Outbox.Append(ctx, OutboxEvent{
		EventID: eventID, EventType: eventType, AggregateID: aggregateID,
		CorrelationID: correlationID, CausationID: causationID,
		OccurredAt: time.Now().UTC(), Version: 1, Data: payload,
	})
}
