CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    player_id TEXT NOT NULL CHECK (length(btrim(player_id)) > 0),
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    balance_minor BIGINT NOT NULL CHECK (balance_minor >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallets_player_currency_unique UNIQUE (player_id, currency),
    CONSTRAINT wallets_id_player_currency_unique UNIQUE (id, player_id, currency),
    CONSTRAINT wallets_id_currency_unique UNIQUE (id, currency)
);
