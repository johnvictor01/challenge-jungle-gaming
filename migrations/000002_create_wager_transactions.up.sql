CREATE TABLE wager_transactions (
    id UUID PRIMARY KEY,
    origin TEXT NOT NULL CHECK (origin IN ('INTERNAL', 'EXTERNAL')),
    wallet_id UUID NOT NULL,
    player_id TEXT NOT NULL,
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    provider_id TEXT,
    external_transaction_id TEXT,
    idempotency_key TEXT,
    payload_hash CHAR(64),
    kind TEXT NOT NULL CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    amount_minor BIGINT NOT NULL,
    round_id TEXT,
    game_id TEXT,
    reference_external_transaction_id TEXT,
    reference_transaction_id UUID REFERENCES wager_transactions(id),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    failure_code TEXT,
    result_balance_minor BIGINT CHECK (result_balance_minor IS NULL OR result_balance_minor >= 0),
    result_currency CHAR(3),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    CONSTRAINT wager_transactions_wallet_player_currency_fk
        FOREIGN KEY (wallet_id, player_id, currency)
        REFERENCES wallets (id, player_id, currency),
    CONSTRAINT wager_transactions_id_wallet_currency_unique UNIQUE (id, wallet_id, currency),
    CONSTRAINT wager_transactions_reference_target_unique
        UNIQUE (id, provider_id, wallet_id, player_id, currency, round_id, external_transaction_id),
    CONSTRAINT wager_transactions_origin_kind_check CHECK (
        (origin = 'INTERNAL' AND kind = 'OPENING'
            AND provider_id IS NULL
            AND external_transaction_id IS NULL
            AND idempotency_key IS NULL
            AND payload_hash IS NULL
            AND round_id IS NULL
            AND game_id IS NULL
            AND reference_external_transaction_id IS NULL
            AND reference_transaction_id IS NULL)
        OR
        (origin = 'EXTERNAL' AND kind <> 'OPENING'
            AND provider_id IS NOT NULL AND length(btrim(provider_id)) > 0
            AND external_transaction_id IS NOT NULL AND length(btrim(external_transaction_id)) > 0
            AND idempotency_key IS NOT NULL AND length(btrim(idempotency_key)) > 0
            AND payload_hash IS NOT NULL AND payload_hash ~ '^[0-9a-f]{64}$'
            AND round_id IS NOT NULL AND length(btrim(round_id)) > 0
            AND game_id IS NOT NULL AND length(btrim(game_id)) > 0)
    ),
    CONSTRAINT wager_transactions_amount_check CHECK (
        (kind = 'LOSS' AND amount_minor = 0)
        OR (kind = 'OPENING' AND amount_minor > 0)
        OR (kind IN ('BET', 'WIN', 'REFUND', 'ROLLBACK') AND amount_minor > 0)
    ),
    CONSTRAINT wager_transactions_reference_check CHECK (
        (kind IN ('REFUND', 'ROLLBACK')
            AND reference_external_transaction_id IS NOT NULL
            AND length(btrim(reference_external_transaction_id)) > 0)
        OR
        (kind = 'WIN'
            AND (reference_external_transaction_id IS NULL
                OR length(btrim(reference_external_transaction_id)) > 0))
        OR
        (kind NOT IN ('WIN', 'REFUND', 'ROLLBACK')
            AND reference_external_transaction_id IS NULL)
    ),
    CONSTRAINT wager_transactions_internal_reference_check CHECK (
        reference_transaction_id IS NULL
        OR (kind IN ('WIN', 'REFUND', 'ROLLBACK') AND reference_external_transaction_id IS NOT NULL)
    ),
    CONSTRAINT wager_transactions_result_currency_check CHECK (
        (result_balance_minor IS NULL AND result_currency IS NULL)
        OR (result_balance_minor IS NOT NULL AND result_currency = currency)
    ),
    CONSTRAINT wager_transactions_terminal_status_check CHECK (
        (status IN ('PROCESSED', 'REJECTED', 'FAILED') AND processed_at IS NOT NULL)
        OR (status IN ('PENDING', 'PENDING_REFERENCE') AND processed_at IS NULL)
    )
);

CREATE UNIQUE INDEX wager_transactions_provider_idempotency_unique
    ON wager_transactions (provider_id, idempotency_key)
    WHERE origin = 'EXTERNAL';

CREATE UNIQUE INDEX wager_transactions_provider_external_id_unique
    ON wager_transactions (provider_id, external_transaction_id)
    WHERE origin = 'EXTERNAL';

CREATE UNIQUE INDEX wager_transactions_one_opening_per_wallet
    ON wager_transactions (wallet_id)
    WHERE origin = 'INTERNAL' AND kind = 'OPENING';

CREATE UNIQUE INDEX wager_transactions_one_reversal_per_reference
    ON wager_transactions (provider_id, reference_external_transaction_id)
    WHERE origin = 'EXTERNAL' AND kind IN ('REFUND', 'ROLLBACK');

ALTER TABLE wager_transactions
    ADD CONSTRAINT wager_transactions_reference_matches_wallet_fk
    FOREIGN KEY (reference_transaction_id, provider_id, wallet_id, player_id, currency, round_id, reference_external_transaction_id)
    REFERENCES wager_transactions (id, provider_id, wallet_id, player_id, currency, round_id, external_transaction_id);

CREATE FUNCTION validate_wager_reference() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    referenced wager_transactions%ROWTYPE;
BEGIN
    IF NEW.reference_transaction_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT * INTO referenced
    FROM wager_transactions
    WHERE id = NEW.reference_transaction_id;

    IF NOT FOUND OR referenced.status <> 'PROCESSED' THEN
        RAISE EXCEPTION 'referenced transaction must exist and be processed';
    END IF;

    IF NEW.amount_minor <> referenced.amount_minor THEN
        IF NEW.kind IN ('REFUND', 'ROLLBACK') THEN
            RAISE EXCEPTION 'reversal amount must equal referenced transaction amount';
        END IF;
    END IF;

    IF (NEW.kind = 'REFUND' AND referenced.kind <> 'BET')
       OR (NEW.kind = 'ROLLBACK' AND referenced.kind NOT IN ('BET', 'WIN', 'REFUND'))
       OR (NEW.kind = 'WIN' AND referenced.kind <> 'BET') THEN
        RAISE EXCEPTION 'referenced transaction kind is not valid for this operation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER wager_transactions_validate_reference
    BEFORE INSERT OR UPDATE OF reference_transaction_id, reference_external_transaction_id, amount_minor, kind
    ON wager_transactions
    FOR EACH ROW EXECUTE FUNCTION validate_wager_reference();

CREATE INDEX wager_transactions_pending_reference_idx
    ON wager_transactions (next_attempt_at, created_at)
    WHERE status = 'PENDING_REFERENCE';

CREATE INDEX wager_transactions_pending_idx
    ON wager_transactions (next_attempt_at, created_at)
    WHERE status = 'PENDING';

CREATE INDEX wager_transactions_wallet_created_idx
    ON wager_transactions (wallet_id, created_at DESC);
