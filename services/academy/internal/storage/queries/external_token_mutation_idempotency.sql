-- The conflict branch takes a row lock until the transaction finishes. A
-- concurrent retry therefore sees either the completed response or a rollback.
-- name: ReserveExternalTokenMutationIdempotency :one
INSERT INTO external_token_mutation_idempotency (
    id, company_id, actor_user_id, operation, idempotency_key,
    request_hash, result_id, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(actor_user_id),
    sqlc.arg(operation), sqlc.arg(idempotency_key),
    sqlc.arg(request_hash), sqlc.arg(result_id), sqlc.arg(created_at)
)
ON CONFLICT (company_id, actor_user_id, operation, idempotency_key)
DO UPDATE SET id = external_token_mutation_idempotency.id
RETURNING id, company_id, actor_user_id, operation, idempotency_key,
    request_hash, result_id, response_payload, completed_at, created_at;

-- name: CompleteExternalTokenMutationIdempotency :one
UPDATE external_token_mutation_idempotency
SET response_payload = sqlc.arg(response_payload),
    completed_at = sqlc.arg(completed_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND completed_at IS NULL
RETURNING id, company_id, actor_user_id, operation, idempotency_key,
    request_hash, result_id, response_payload, completed_at, created_at;
