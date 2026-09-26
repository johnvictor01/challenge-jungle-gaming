DROP INDEX wager_transactions_one_reversal_per_reference;

-- Rejected reversals remain in the audit history but must not prevent a later
-- valid reversal. Pending and processed reversals still reserve the reference.
CREATE UNIQUE INDEX wager_transactions_one_reversal_per_reference
    ON wager_transactions (provider_id, reference_external_transaction_id)
    WHERE origin = 'EXTERNAL'
      AND kind IN ('REFUND', 'ROLLBACK')
      AND status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED');
