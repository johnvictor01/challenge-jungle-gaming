DROP INDEX wager_transactions_one_reversal_per_reference;

CREATE UNIQUE INDEX wager_transactions_one_reversal_per_reference
    ON wager_transactions (provider_id, reference_external_transaction_id)
    WHERE origin = 'EXTERNAL' AND kind IN ('REFUND', 'ROLLBACK');
