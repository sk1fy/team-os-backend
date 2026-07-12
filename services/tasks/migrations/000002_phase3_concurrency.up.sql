ALTER TABLE tasks ADD COLUMN IF NOT EXISTS recurrence_generated_at timestamptz;

ALTER TABLE outbox ADD COLUMN IF NOT EXISTS aggregate_id uuid;
ALTER TABLE outbox ADD COLUMN IF NOT EXISTS event_order bigserial;

UPDATE outbox
SET aggregate_id = COALESCE(
    NULLIF(payload -> 'payload' ->> 'taskId', '')::uuid,
    NULLIF(payload -> 'payload' ->> 'sourceEntityId', '')::uuid,
    id
)
WHERE aggregate_id IS NULL;

WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY occurred_at, id) AS position
    FROM outbox
)
UPDATE outbox
SET event_order = ordered.position
FROM ordered
WHERE outbox.id = ordered.id;

SELECT setval(
    pg_get_serial_sequence('outbox', 'event_order'),
    COALESCE(MAX(event_order), 0) + 1,
    false
)
FROM outbox;

ALTER TABLE outbox ALTER COLUMN aggregate_id SET NOT NULL;
ALTER TABLE outbox ALTER COLUMN event_order SET NOT NULL;

DROP INDEX IF EXISTS outbox_unpublished_idx;
CREATE INDEX outbox_unpublished_idx
    ON outbox (company_id, aggregate_id, event_order, next_attempt_at)
    WHERE published_at IS NULL;
