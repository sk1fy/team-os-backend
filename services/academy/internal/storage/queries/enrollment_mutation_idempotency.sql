-- The conflict branch takes a row lock until the surrounding transaction ends.
-- This serializes concurrent retries with the same key.
-- name: ReserveEnrollmentMutationIdempotency :one
INSERT INTO enrollment_mutation_idempotency (
    id, company_id, enrollment_id, actor_user_id, operation,
    idempotency_key, request_hash, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(enrollment_id),
    sqlc.arg(actor_user_id), sqlc.arg(operation),
    sqlc.arg(idempotency_key), sqlc.arg(request_hash), sqlc.arg(created_at)
)
ON CONFLICT (company_id, actor_user_id, operation, idempotency_key)
DO UPDATE SET id = enrollment_mutation_idempotency.id
RETURNING id, company_id, enrollment_id, actor_user_id, operation,
    idempotency_key, request_hash, result_id, completed_at, created_at;

-- name: CompleteEnrollmentMutationIdempotency :one
UPDATE enrollment_mutation_idempotency
SET result_id = sqlc.narg(result_id),
    completed_at = sqlc.arg(completed_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND completed_at IS NULL
RETURNING id, company_id, enrollment_id, actor_user_id, operation,
    idempotency_key, request_hash, result_id, completed_at, created_at;
