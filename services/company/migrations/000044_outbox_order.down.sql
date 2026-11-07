DROP INDEX outbox_company_pending_order_idx;
ALTER TABLE outbox DROP COLUMN event_order;
