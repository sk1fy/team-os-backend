-- name: CreateOutboxEvent :one
INSERT INTO outbox (id, company_id, aggregate_id, subject, payload, headers, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, company_id, aggregate_id, event_order, subject, payload, headers, occurred_at,
          next_attempt_at, published_at, attempts, last_error;
