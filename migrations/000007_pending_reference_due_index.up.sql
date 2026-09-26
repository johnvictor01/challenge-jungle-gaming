CREATE INDEX wager_transactions_pending_reference_due_idx
    ON wager_transactions (COALESCE(next_attempt_at, created_at), created_at, id)
    WHERE status = 'PENDING_REFERENCE';
