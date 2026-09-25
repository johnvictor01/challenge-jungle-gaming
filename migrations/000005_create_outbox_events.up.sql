CREATE TABLE outbox_events (
    event_id UUID PRIMARY KEY,
    aggregate_id UUID NOT NULL,
    event_type TEXT NOT NULL CHECK (length(btrim(event_type)) > 0),
    event_version INTEGER NOT NULL CHECK (event_version > 0),
    correlation_id TEXT NOT NULL CHECK (length(btrim(correlation_id)) > 0),
    causation_id TEXT,
    payload JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_owner TEXT,
    lease_until TIMESTAMPTZ,
    last_error TEXT,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT outbox_events_lease_pair_check CHECK (
        (lease_owner IS NULL AND lease_until IS NULL)
        OR (lease_owner IS NOT NULL AND lease_until IS NOT NULL)
    )
);

CREATE INDEX outbox_events_ready_idx
    ON outbox_events (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX outbox_events_aggregate_idx
    ON outbox_events (aggregate_id, occurred_at);

CREATE FUNCTION protect_outbox_event_snapshot() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.event_id IS DISTINCT FROM OLD.event_id
       OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
       OR NEW.event_type IS DISTINCT FROM OLD.event_type
       OR NEW.event_version IS DISTINCT FROM OLD.event_version
       OR NEW.correlation_id IS DISTINCT FROM OLD.correlation_id
       OR NEW.causation_id IS DISTINCT FROM OLD.causation_id
       OR NEW.payload IS DISTINCT FROM OLD.payload
       OR NEW.occurred_at IS DISTINCT FROM OLD.occurred_at THEN
        RAISE EXCEPTION 'outbox event identity and payload are immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER outbox_events_snapshot_immutable
    BEFORE UPDATE ON outbox_events
    FOR EACH ROW EXECUTE FUNCTION protect_outbox_event_snapshot();
