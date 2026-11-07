ALTER TABLE outbox ADD COLUMN event_order bigserial;

WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY occurred_at, id) AS sequence
    FROM outbox
)
UPDATE outbox SET event_order = ordered.sequence FROM ordered WHERE outbox.id = ordered.id;

SELECT setval(pg_get_serial_sequence('outbox', 'event_order'), COALESCE(MAX(event_order), 0) + 1, false)
FROM outbox;

ALTER TABLE outbox ALTER COLUMN event_order SET NOT NULL;
CREATE INDEX outbox_company_pending_order_idx ON outbox(company_id, event_order)
WHERE published_at IS NULL;
