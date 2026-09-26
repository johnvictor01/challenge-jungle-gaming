DROP INDEX outbox_events_failed_idx;
ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_lease_pair_check;
ALTER TABLE outbox_events ADD CONSTRAINT outbox_events_lease_pair_check CHECK (
    (lease_owner IS NULL AND lease_until IS NULL)
    OR (lease_owner IS NOT NULL AND lease_until IS NOT NULL)
);
ALTER TABLE outbox_events DROP COLUMN failed;
