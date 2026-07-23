-- name: UpsertExternalLearner :one
INSERT INTO external_learners (
    id, company_id, email, normalized_email, first_name, last_name, phone,
    email_verified_at, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(email),
    sqlc.arg(normalized_email), sqlc.narg(first_name), sqlc.narg(last_name),
    sqlc.narg(phone), sqlc.arg(email_verified_at),
    sqlc.arg(created_at), sqlc.arg(updated_at)
)
ON CONFLICT (company_id, normalized_email) DO UPDATE
SET email = EXCLUDED.email,
    first_name = EXCLUDED.first_name,
    last_name = COALESCE(EXCLUDED.last_name, external_learners.last_name),
    phone = COALESCE(EXCLUDED.phone, external_learners.phone),
    email_verified_at = COALESCE(
        external_learners.email_verified_at, EXCLUDED.email_verified_at
    ),
    updated_at = EXCLUDED.updated_at,
    deleted_at = NULL
RETURNING id, company_id, email, normalized_email, first_name, last_name,
    phone, email_verified_at, created_at, updated_at, deleted_at;

-- name: GetExternalLearner :one
SELECT id, company_id, email, normalized_email, first_name, last_name,
    phone, email_verified_at, created_at, updated_at, deleted_at
FROM external_learners
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: GetExternalLearnerByEmail :one
SELECT id, company_id, email, normalized_email, first_name, last_name,
    phone, email_verified_at, created_at, updated_at, deleted_at
FROM external_learners
WHERE company_id = sqlc.arg(company_id)
  AND normalized_email = sqlc.arg(normalized_email)
  AND deleted_at IS NULL;

-- name: VerifyExternalLearnerEmail :one
UPDATE external_learners
SET email_verified_at = COALESCE(email_verified_at, sqlc.arg(verified_at)),
    updated_at = sqlc.arg(verified_at),
    deleted_at = NULL
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
RETURNING id, company_id, email, normalized_email, first_name, last_name,
    phone, email_verified_at, created_at, updated_at, deleted_at;

-- name: SoftDeleteExternalLearner :execrows
UPDATE external_learners
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(deleted_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: CreateExternalVerificationChallenge :one
INSERT INTO external_verification_challenges (
    id, company_id, normalized_email, purpose, source_id,
    claimed_first_name, claimed_last_name, code_hash, request_ip_hash,
    expires_at, attempts, max_attempts, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(normalized_email),
    sqlc.arg(purpose), sqlc.narg(source_id), sqlc.narg(claimed_first_name),
    sqlc.narg(claimed_last_name), sqlc.arg(code_hash),
    sqlc.narg(request_ip_hash), sqlc.arg(expires_at), 0,
    sqlc.arg(max_attempts), sqlc.arg(created_at)
)
RETURNING id, company_id, normalized_email, purpose, source_id,
    claimed_first_name, claimed_last_name, code_hash, request_ip_hash,
    expires_at, attempts, max_attempts, consumed_at, invalidated_at,
    invalidation_reason, created_at;

-- name: CountRecentExternalChallengesByEmail :one
SELECT count(*)::integer
FROM external_verification_challenges
WHERE company_id = sqlc.arg(company_id)
  AND normalized_email = sqlc.arg(normalized_email)
  AND created_at >= sqlc.arg(since);

-- name: CountRecentExternalChallengesBySource :one
SELECT count(*)::integer
FROM external_verification_challenges
WHERE company_id = sqlc.arg(company_id)
  AND purpose = sqlc.arg(purpose)
  AND source_id = sqlc.arg(source_id)
  AND created_at >= sqlc.arg(since);

-- name: CountRecentExternalChallengesByIPHash :one
SELECT count(*)::integer
FROM external_verification_challenges
WHERE company_id = sqlc.arg(company_id)
  AND request_ip_hash = sqlc.arg(request_ip_hash)
  AND created_at >= sqlc.arg(since);

-- name: InvalidateOpenExternalChallenges :execrows
UPDATE external_verification_challenges
SET invalidated_at = sqlc.arg(invalidated_at),
    invalidation_reason = sqlc.arg(invalidation_reason)
WHERE company_id = sqlc.arg(company_id)
  AND normalized_email = sqlc.arg(normalized_email)
  AND purpose = sqlc.arg(purpose)
  AND source_id IS NOT DISTINCT FROM sqlc.narg(source_id)::uuid
  AND consumed_at IS NULL
  AND invalidated_at IS NULL
  AND id <> sqlc.arg(except_id);

-- name: GetExternalVerificationChallengeForUpdate :one
SELECT id, company_id, normalized_email, purpose, source_id,
    claimed_first_name, claimed_last_name, code_hash, request_ip_hash,
    expires_at, attempts, max_attempts, consumed_at, invalidated_at,
    invalidation_reason, created_at
FROM external_verification_challenges
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
FOR UPDATE;

-- Challenge UUID is returned by the public request-verification endpoint and
-- is the tenant bootstrap for confirm. The code still has to be verified in
-- constant time by the application before the row can be consumed.
-- name: ResolveExternalVerificationChallengeForUpdate :one
SELECT id, company_id, normalized_email, purpose, source_id,
    claimed_first_name, claimed_last_name, code_hash, request_ip_hash,
    expires_at, attempts, max_attempts, consumed_at, invalidated_at,
    invalidation_reason, created_at
FROM external_verification_challenges
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: RecordExternalVerificationFailure :one
UPDATE external_verification_challenges
SET attempts = attempts + 1,
    invalidated_at = CASE
        WHEN attempts + 1 >= max_attempts THEN sqlc.arg(attempted_at)
        ELSE invalidated_at
    END,
    invalidation_reason = CASE
        WHEN attempts + 1 >= max_attempts THEN 'attempts_exhausted'
        ELSE invalidation_reason
    END
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND consumed_at IS NULL
  AND invalidated_at IS NULL
  AND expires_at > sqlc.arg(attempted_at)
  AND attempts < max_attempts
RETURNING id, company_id, normalized_email, purpose, source_id,
    claimed_first_name, claimed_last_name, code_hash, request_ip_hash,
    expires_at, attempts, max_attempts, consumed_at, invalidated_at,
    invalidation_reason, created_at;

-- name: ConsumeExternalVerificationChallenge :one
UPDATE external_verification_challenges
SET consumed_at = sqlc.arg(consumed_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND consumed_at IS NULL
  AND invalidated_at IS NULL
  AND expires_at > sqlc.arg(consumed_at)
  AND attempts < max_attempts
RETURNING id, company_id, normalized_email, purpose, source_id,
    claimed_first_name, claimed_last_name, code_hash, request_ip_hash,
    expires_at, attempts, max_attempts, consumed_at, invalidated_at,
    invalidation_reason, created_at;

-- name: MaterializeExpiredExternalChallenges :many
UPDATE external_verification_challenges AS challenge
SET invalidated_at = challenge.expires_at,
    invalidation_reason = 'expired'
WHERE challenge.id IN (
    SELECT candidate.id
    FROM external_verification_challenges AS candidate
    WHERE candidate.company_id = sqlc.arg(company_id)
      AND candidate.consumed_at IS NULL
      AND candidate.invalidated_at IS NULL
      AND candidate.expires_at <= sqlc.arg(now)
    ORDER BY candidate.expires_at, candidate.id
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
)
RETURNING id, company_id, normalized_email, purpose, source_id,
    claimed_first_name, claimed_last_name, expires_at, attempts, max_attempts,
    consumed_at, invalidated_at, invalidation_reason, created_at;

-- name: CreateExternalSession :one
INSERT INTO external_sessions (
    id, company_id, external_learner_id, token_hash,
    expires_at, last_used_at, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(external_learner_id),
    sqlc.arg(token_hash), sqlc.arg(expires_at), sqlc.arg(created_at),
    sqlc.arg(created_at)
)
RETURNING id, company_id, external_learner_id, token_hash, expires_at,
    last_used_at, revoked_at, revocation_reason, created_at;

-- name: GetExternalSessionByTokenHash :one
SELECT session.id, session.company_id, session.external_learner_id,
    session.token_hash, session.expires_at, session.last_used_at,
    session.revoked_at, session.revocation_reason, session.created_at
FROM external_sessions AS session
JOIN external_learners AS learner
  ON learner.company_id = session.company_id
 AND learner.id = session.external_learner_id
WHERE session.company_id = sqlc.arg(company_id)
  AND session.token_hash = sqlc.arg(token_hash)
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
  AND learner.deleted_at IS NULL;

-- Resolve is the only tenant-bootstrap query. token_hash is globally unique;
-- every operation after this lookup must use the returned company_id.
-- name: ResolveExternalSessionByTokenHash :one
SELECT session.id, session.company_id, session.external_learner_id,
    session.token_hash, session.expires_at, session.last_used_at,
    session.revoked_at, session.revocation_reason, session.created_at
FROM external_sessions AS session
JOIN external_learners AS learner
  ON learner.company_id = session.company_id
 AND learner.id = session.external_learner_id
WHERE session.token_hash = sqlc.arg(token_hash)
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
  AND learner.deleted_at IS NULL;

-- name: GetActiveExternalSessionForEmail :one
SELECT session.id, session.company_id, session.external_learner_id,
    session.token_hash, session.expires_at, session.last_used_at,
    session.revoked_at, session.revocation_reason, session.created_at
FROM external_sessions AS session
JOIN external_learners AS learner
  ON learner.company_id = session.company_id
 AND learner.id = session.external_learner_id
WHERE session.company_id = sqlc.arg(company_id)
  AND learner.normalized_email = sqlc.arg(normalized_email)
  AND learner.deleted_at IS NULL
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
ORDER BY session.last_used_at DESC NULLS LAST, session.created_at DESC, session.id DESC
LIMIT 1;

-- name: TouchExternalSession :one
UPDATE external_sessions
SET last_used_at = GREATEST(
        COALESCE(last_used_at, created_at), sqlc.arg(last_used_at)
    )
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(last_used_at)
RETURNING id, company_id, external_learner_id, token_hash, expires_at,
    last_used_at, revoked_at, revocation_reason, created_at;

-- name: RevokeExternalSession :one
UPDATE external_sessions
SET revoked_at = sqlc.arg(revoked_at),
    revocation_reason = sqlc.arg(revocation_reason)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND revoked_at IS NULL
RETURNING id, company_id, external_learner_id, token_hash, expires_at,
    last_used_at, revoked_at, revocation_reason, created_at;

-- name: RevokeExpiredExternalSessions :many
UPDATE external_sessions AS session
SET revoked_at = session.expires_at,
    revocation_reason = 'expired'
WHERE session.id IN (
    SELECT candidate.id
    FROM external_sessions AS candidate
    WHERE candidate.company_id = sqlc.arg(company_id)
      AND candidate.revoked_at IS NULL
      AND candidate.expires_at <= sqlc.arg(now)
    ORDER BY candidate.expires_at, candidate.id
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
)
RETURNING id, company_id, external_learner_id, expires_at, revoked_at,
    revocation_reason, created_at;

-- name: ReserveExternalMutationIdempotency :one
INSERT INTO external_mutation_idempotency (
    id, company_id, external_learner_id, operation, idempotency_key,
    request_hash, aggregate_id, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(external_learner_id),
    sqlc.arg(operation), sqlc.arg(idempotency_key), sqlc.arg(request_hash),
    sqlc.arg(aggregate_id), sqlc.arg(created_at)
)
ON CONFLICT (company_id, external_learner_id, operation, idempotency_key)
DO UPDATE SET id = external_mutation_idempotency.id
RETURNING id, company_id, external_learner_id, operation, idempotency_key,
    request_hash, aggregate_id, result_id, enrollment_id, result_payload,
    completed_at, created_at;

-- name: GetExternalMutationIdempotencyForUpdate :one
SELECT id, company_id, external_learner_id, operation, idempotency_key,
    request_hash, aggregate_id, result_id, enrollment_id, result_payload,
    completed_at, created_at
FROM external_mutation_idempotency
WHERE company_id = sqlc.arg(company_id)
  AND external_learner_id = sqlc.arg(external_learner_id)
  AND operation = sqlc.arg(operation)
  AND idempotency_key = sqlc.arg(idempotency_key)
FOR UPDATE;

-- name: CompleteExternalMutationIdempotency :one
UPDATE external_mutation_idempotency
SET result_id = sqlc.narg(result_id),
    enrollment_id = sqlc.narg(enrollment_id),
    result_payload = sqlc.arg(result_payload),
    completed_at = sqlc.arg(completed_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND completed_at IS NULL
RETURNING id, company_id, external_learner_id, operation, idempotency_key,
    request_hash, aggregate_id, result_id, enrollment_id, result_payload,
    completed_at, created_at;
