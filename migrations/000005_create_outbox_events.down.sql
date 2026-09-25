DROP TRIGGER outbox_events_snapshot_immutable ON outbox_events;
DROP TABLE outbox_events;
DROP FUNCTION protect_outbox_event_snapshot();
