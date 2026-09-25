CREATE TABLE inbox_messages (
    consumer_name TEXT NOT NULL CHECK (length(btrim(consumer_name)) > 0),
    message_id TEXT NOT NULL CHECK (length(btrim(message_id)) > 0),
    payload_hash CHAR(64) NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    transaction_id UUID REFERENCES wager_transactions (id),
    PRIMARY KEY (consumer_name, message_id)
);

CREATE INDEX inbox_messages_received_idx ON inbox_messages (received_at);
