-- name: CreateOutboxEvent :one
INSERT INTO outbox (id, company_id, subject, payload, headers, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, company_id, subject, payload, headers, occurred_at, next_attempt_at, published_at, attempts, last_error;