-- name: LockAmoAccount :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        sqlc.arg('provider')::text || ':' || sqlc.arg('external_account_id')::text,
        0
    )
);

-- name: CreateCompanyIntegration :one
INSERT INTO company_integrations (
    id, company_id, provider, external_account_id, app_name, entitlements,
    status, last_verified_at, metadata
)
VALUES (
    sqlc.arg('id'), sqlc.arg('company_id'), sqlc.arg('provider'),
    sqlc.arg('external_account_id'), sqlc.narg('app_name'),
    sqlc.arg('entitlements'), 'active', sqlc.narg('last_verified_at'),
    sqlc.arg('metadata')
)
RETURNING *;

-- name: GetCompanyIntegrationByExternalAccount :one
SELECT *
FROM company_integrations
WHERE provider = sqlc.arg('provider')
  AND external_account_id = sqlc.arg('external_account_id');

-- name: AmoAccountExists :one
SELECT (
    EXISTS (
        SELECT 1
        FROM company_integrations AS integration
        WHERE integration.provider = sqlc.arg('provider')
          AND integration.external_account_id = sqlc.arg('external_account_id')
    )
    OR EXISTS (
        SELECT 1
        FROM company_registration_tokens AS registration_token
        WHERE registration_token.provider = sqlc.arg('provider')
          AND registration_token.external_account_id = sqlc.arg('external_account_id')
          AND registration_token.consumed_at IS NULL
          AND registration_token.revoked_at IS NULL
          AND registration_token.expires_at > sqlc.arg('now')
    )
)::boolean;

-- name: GetActiveCompanyRegistrationTokenForAccount :one
SELECT *
FROM company_registration_tokens AS registration_token
WHERE registration_token.provider = sqlc.arg('provider')
  AND registration_token.external_account_id = sqlc.arg('external_account_id')
  AND registration_token.consumed_at IS NULL
  AND registration_token.revoked_at IS NULL
FOR UPDATE;

-- name: CreateCompanyRegistrationToken :one
INSERT INTO company_registration_tokens (
    id, company_id, provider, external_account_id, token_hash, expires_at, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetCompanyRegistrationTokenByHash :one
SELECT *
FROM company_registration_tokens
WHERE token_hash = $1;

-- name: GetCompanyRegistrationTokenByHashForUpdate :one
SELECT *
FROM company_registration_tokens
WHERE token_hash = $1
FOR UPDATE;

-- name: RevokeCompanyRegistrationToken :one
UPDATE company_registration_tokens
SET revoked_at = sqlc.arg('revoked_at'),
    revocation_reason = sqlc.arg('revocation_reason')
WHERE id = sqlc.arg('id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: ConsumeCompanyRegistrationToken :one
UPDATE company_registration_tokens
SET consumed_at = sqlc.arg('consumed_at')
WHERE id = sqlc.arg('id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: DeleteOldCompanyRegistrationTokens :execrows
DELETE FROM company_registration_tokens
WHERE expires_at < sqlc.arg('before');

-- name: CreateCompanyFromRegistrationToken :one
INSERT INTO companies (
    id, name, logo_url, amo_account_id, status, onboarding_completed_at
)
VALUES (
    sqlc.arg('id'), sqlc.arg('name'), sqlc.narg('logo_url'),
    sqlc.arg('amo_account_id'), 'active', sqlc.arg('completed_at')
)
RETURNING *;
