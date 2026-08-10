-- name: GetAmoWidgetIntegrationForUpdate :one
SELECT sqlc.embed(integration), sqlc.embed(company)
FROM company_integrations AS integration
JOIN companies AS company ON company.id = integration.company_id
WHERE integration.provider = sqlc.arg('provider')
  AND integration.external_account_id = sqlc.arg('external_account_id')
FOR UPDATE OF integration, company;

-- name: ListLegacyAmoWidgetCompaniesForUpdate :many
SELECT *
FROM companies
WHERE amo_account_id = sqlc.arg('external_account_id')
ORDER BY created_at, id
FOR UPDATE;

-- name: GetAmoWidgetUserByIdentity :one
SELECT sqlc.embed(u), sqlc.embed(identity)
FROM user_external_identities AS identity
JOIN users AS u
  ON u.company_id = identity.company_id
 AND u.id = identity.user_id
WHERE identity.company_id = sqlc.arg('company_id')
  AND identity.integration_id = sqlc.arg('integration_id')
  AND identity.provider = sqlc.arg('provider')
  AND identity.external_user_id = sqlc.arg('external_user_id')
FOR UPDATE OF identity, u;

-- name: FindAmoWidgetUserForUpdate :one
SELECT *
FROM users
WHERE company_id = sqlc.arg('company_id')
  AND external_deleted_at IS NULL
  AND external_id = sqlc.arg('external_id')
LIMIT 1
FOR UPDATE;

-- name: PromoteAmoWidgetOwner :one
UPDATE users
SET role = 'owner',
    status = 'active',
    show_in_schedule = false,
    updated_at = sqlc.arg('updated_at')
WHERE company_id = sqlc.arg('company_id')
  AND id = sqlc.arg('user_id')
  AND external_deleted_at IS NULL
RETURNING *;

-- name: CreateAmoWidgetIdentity :one
INSERT INTO user_external_identities (
    id, company_id, integration_id, user_id, provider, external_account_id,
    external_user_id, status, last_verified_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8)
RETURNING *;

-- name: ActivateAmoWidgetIdentity :one
UPDATE user_external_identities
SET status = 'active',
    last_verified_at = sqlc.arg('verified_at'),
    updated_at = sqlc.arg('verified_at')
WHERE company_id = sqlc.arg('company_id')
  AND id = sqlc.arg('identity_id')
RETURNING *;

-- name: AmoWidgetUserHasPassword :one
SELECT EXISTS (
    SELECT 1
    FROM credentials
    WHERE company_id = sqlc.arg('company_id')
      AND user_id = sqlc.arg('user_id')
)::boolean;

-- name: RevokeActiveAmoWidgetContinuations :execrows
UPDATE sso_tokens
SET revoked_at = sqlc.arg('revoked_at')
WHERE external_identity_id = sqlc.arg('external_identity_id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL;

-- name: CreateAmoWidgetContinuation :one
INSERT INTO sso_tokens (
    id, company_id, user_id, external_identity_id, token_hash, expires_at, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetAmoWidgetContinuation :one
SELECT token.id AS token_id,
       token.expires_at,
       token.consumed_at,
       token.revoked_at,
       u.email,
       company.name AS company_name,
       EXISTS (
           SELECT 1 FROM credentials AS credential
           WHERE credential.company_id = token.company_id
             AND credential.user_id = token.user_id
       )::boolean AS has_password,
       u.status AS user_status,
       u.external_deleted_at,
       identity.status AS identity_status,
       integration.status AS integration_status,
       company.status AS company_status
FROM sso_tokens AS token
JOIN user_external_identities AS identity
  ON identity.company_id = token.company_id
 AND identity.user_id = token.user_id
 AND identity.id = token.external_identity_id
JOIN company_integrations AS integration
  ON integration.company_id = identity.company_id
 AND integration.id = identity.integration_id
JOIN users AS u
  ON u.company_id = token.company_id
 AND u.id = token.user_id
JOIN companies AS company ON company.id = token.company_id
WHERE token.token_hash = sqlc.arg('token_hash');

-- name: GetAmoWidgetContinuationForUpdate :one
SELECT token.id AS token_id,
       token.expires_at,
       token.consumed_at,
       token.revoked_at,
       sqlc.embed(u),
       COALESCE(credential.password_hash, '')::text AS password_hash,
       identity.status AS identity_status,
       integration.provider,
       integration.external_account_id,
       identity.external_user_id,
       integration.status AS integration_status,
       company.status AS company_status
FROM sso_tokens AS token
JOIN user_external_identities AS identity
  ON identity.company_id = token.company_id
 AND identity.user_id = token.user_id
 AND identity.id = token.external_identity_id
JOIN company_integrations AS integration
  ON integration.company_id = identity.company_id
 AND integration.id = identity.integration_id
JOIN users AS u
  ON u.company_id = token.company_id
 AND u.id = token.user_id
JOIN companies AS company ON company.id = token.company_id
LEFT JOIN credentials AS credential
  ON credential.company_id = token.company_id
 AND credential.user_id = token.user_id
WHERE token.token_hash = sqlc.arg('token_hash')
FOR UPDATE OF token, identity, integration, u, company;

-- name: ConsumeAmoWidgetContinuation :one
UPDATE sso_tokens
SET consumed_at = sqlc.arg('consumed_at')
WHERE id = sqlc.arg('token_id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg('consumed_at')
RETURNING *;

-- name: DeleteExpiredAmoWidgetContinuations :execrows
DELETE FROM sso_tokens
WHERE expires_at < sqlc.arg('before');
