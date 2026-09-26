package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInboxPayloadConflict = errors.New("SQS message ID was reused with a different payload")
	ErrInboxIncomplete      = errors.New("SQS inbox message is not durably completed")
)

type InboxWagerCommand struct {
	ConsumerName string
	MessageID    string
	PayloadHash  string
	Wager        ProcessWagerCommand
}

type InboxWagerResult struct {
	Wager             ProcessWagerResult
	DuplicateDelivery bool
}

// ProcessInboxWagerService shares one SQL transaction between the inbox receipt
// and the existing idempotent wager use case.
type ProcessInboxWagerService struct {
	uow       InboxUnitOfWork
	processor *ProcessWagerService
	now       func() time.Time
}

func NewProcessInboxWagerService(uow InboxUnitOfWork, processor *ProcessWagerService) *ProcessInboxWagerService {
	return &ProcessInboxWagerService{uow: uow, processor: processor, now: time.Now}
}

func (s *ProcessInboxWagerService) Execute(ctx context.Context, command InboxWagerCommand) (InboxWagerResult, error) {
	if s == nil || s.uow == nil || s.processor == nil {
		return InboxWagerResult{}, errors.New("inbox wager service is not configured")
	}
	command.ConsumerName, command.MessageID = strings.TrimSpace(command.ConsumerName), strings.TrimSpace(command.MessageID)
	if command.ConsumerName == "" || command.MessageID == "" {
		return InboxWagerResult{}, errors.New("consumer name and SQS message ID are required")
	}
	hash, err := CanonicalPayloadHash(command.Wager)
	if err != nil {
		return InboxWagerResult{}, err
	}
	if command.PayloadHash != "" && command.PayloadHash != hash {
		return InboxWagerResult{}, ErrPayloadHashMismatch
	}
	command.PayloadHash = hash
	var result InboxWagerResult
	err = s.uow.WithinInboxTransaction(ctx, command.ConsumerName, command.MessageID, hash, func(repositories Repositories, duplicate bool) error {
		if duplicate {
			message, err := repositories.Inbox.Find(ctx, command.ConsumerName, command.MessageID)
			if err != nil {
				return err
			}
			if message.PayloadHash != hash {
				return ErrInboxPayloadConflict
			}
			if message.CompletedAt == nil || message.TransactionID == "" {
				return ErrInboxIncomplete
			}
			transaction, err := repositories.Transactions.FindByID(ctx, message.TransactionID)
			if err != nil {
				return err
			}
			result.Wager = resultFromTransaction(transaction, true)
			result.DuplicateDelivery = true
			return nil
		}
		command.Wager.PayloadHash = hash
		var processErr error
		result.Wager, processErr = s.processor.executeInTransaction(ctx, repositories, command.Wager)
		if processErr != nil {
			return processErr
		}
		if err := repositories.Inbox.Complete(ctx, command.ConsumerName, command.MessageID, result.Wager.TransactionID, s.now()); err != nil {
			return fmt.Errorf("complete inbox message: %w", err)
		}
		return nil
	})
	if err != nil {
		return InboxWagerResult{}, err
	}
	return result, nil
}
