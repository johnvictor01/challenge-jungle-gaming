CREATE TABLE wallet_ledger_entries (
    id UUID PRIMARY KEY,
    wallet_id UUID NOT NULL,
    transaction_id UUID NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    balance_before_minor BIGINT NOT NULL CHECK (balance_before_minor >= 0),
    balance_after_minor BIGINT NOT NULL CHECK (balance_after_minor >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallet_ledger_entries_wallet_transaction_unique UNIQUE (wallet_id, transaction_id),
    CONSTRAINT wallet_ledger_entries_wallet_currency_fk
        FOREIGN KEY (wallet_id, currency) REFERENCES wallets (id, currency),
    CONSTRAINT wallet_ledger_entries_transaction_fk
        FOREIGN KEY (transaction_id, wallet_id, currency)
        REFERENCES wager_transactions (id, wallet_id, currency),
    CONSTRAINT wallet_ledger_entries_balance_math_check CHECK (
        (direction = 'DEBIT' AND balance_before_minor >= amount_minor
            AND balance_after_minor = balance_before_minor - amount_minor)
        OR
        (direction = 'CREDIT'
            AND balance_after_minor::NUMERIC = balance_before_minor::NUMERIC + amount_minor::NUMERIC)
    )
);

CREATE INDEX wallet_ledger_entries_wallet_created_idx
    ON wallet_ledger_entries (wallet_id, created_at, id);

CREATE FUNCTION reject_wallet_ledger_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'wallet ledger entries are append-only';
END;
$$;

CREATE TRIGGER wallet_ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION reject_wallet_ledger_mutation();

CREATE TRIGGER wallet_ledger_entries_no_truncate
    BEFORE TRUNCATE ON wallet_ledger_entries
    FOR EACH STATEMENT EXECUTE FUNCTION reject_wallet_ledger_mutation();
