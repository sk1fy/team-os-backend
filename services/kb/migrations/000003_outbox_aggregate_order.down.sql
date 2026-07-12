DROP INDEX IF EXISTS outbox_unpublished_idx;
CREATE INDEX outbox_unpublished_idx ON outbox (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;
ALTER TABLE outbox DROP COLUMN IF EXISTS aggregate_id;
ALTER TABLE outbox DROP COLUMN IF EXISTS event_order;
