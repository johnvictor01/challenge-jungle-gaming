ALTER TABLE outbox_events
    ADD COLUMN failed BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_lease_pair_check;
ALTER TABLE outbox_events ADD CONSTRAINT outbox_events_lease_pair_check CHECK (
    (lease_owner IS NULL AND lease_until IS NULL)
    OR (lease_owner IS NOT NULL AND lease_until IS NOT NULL AND NOT failed)
);

CREATE INDEX outbox_events_failed_idx
    ON outbox_events (failed, next_attempt_at, occurred_at)
    WHERE published_at IS NULL;
