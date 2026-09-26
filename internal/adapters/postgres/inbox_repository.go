package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/application"
)

type inboxRepository struct{ tx pgx.Tx }

func (r inboxRepository) Find(ctx context.Context, consumerName, messageID string) (application.InboxMessage, error) {
	var message application.InboxMessage
	var completedAt *time.Time
	var transactionID *string
	err := r.tx.QueryRow(ctx, `SELECT consumer_name, message_id, payload_hash, received_at, completed_at, transaction_id
		FROM inbox_messages WHERE consumer_name = $1 AND message_id = $2`, consumerName, messageID).
		Scan(&message.ConsumerName, &message.MessageID, &message.PayloadHash, &message.ReceivedAt, &completedAt, &transactionID)
	if err != nil {
		return application.InboxMessage{}, mapError(err)
	}
	message.CompletedAt = completedAt
	message.TransactionID = valueOf(transactionID)
	return message, nil
}

func (r inboxRepository) Complete(ctx context.Context, consumerName, messageID, transactionID string, completedAt time.Time) error {
	var txID any
	if transactionID != "" {
		txID = transactionID
	}
	tag, err := r.tx.Exec(ctx, `UPDATE inbox_messages SET completed_at = $3, transaction_id = $4
		WHERE consumer_name = $1 AND message_id = $2`, consumerName, messageID, completedAt, txID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return application.ErrNotFound
	}
	return nil
}

var _ application.InboxRepository = inboxRepository{}
