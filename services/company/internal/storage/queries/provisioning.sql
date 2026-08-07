-- name: LockProvisioningAccount :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        sqlc.arg('provider')::text || ':' || sqlc.arg('external_account_id')::text,
        0
    )
);

-- name: LockProvisioningKey :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        sqlc.arg('provider')::text || ':idempotency:' || sqlc.arg('idempotency_key')::text,
        0
    )
);

-- name: CreateProvisioningCompany :one
INSERT INTO companies (
    id, name, logo_url, amo_account_id, status, onboarding_completed_at
)
VALUES ($1, $2, $3, sqlc.narg('amo_account_id'), 'onboarding', NULL)
RETURNING *;

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

-- name: GetCompanyIntegrationByExternalAccountForUpdate :one
SELECT *
FROM company_integrations
WHERE provider = sqlc.arg('provider')
  AND external_account_id = sqlc.arg('external_account_id')
FOR UPDATE;

-- name: GetProvisionedCompanyStatus :one
SELECT company.id AS company_id, company.status AS company_status
FROM company_integrations AS integration
JOIN companies AS company ON company.id = integration.company_id
WHERE integration.provider = sqlc.arg('provider')
  AND integration.external_account_id = sqlc.arg('external_account_id');

-- name: UpdateCompanyIntegrationStatus :one
UPDATE company_integrations
SET status = sqlc.arg('status'),
    frozen_at = CASE
        WHEN sqlc.arg('status')::text = 'frozen' THEN COALESCE(frozen_at, sqlc.arg('changed_at'))
        ELSE NULL
    END,
    last_verified_at = CASE
        WHEN sqlc.arg('mark_verified')::boolean THEN sqlc.arg('changed_at')
        ELSE last_verified_at
    END,
    updated_at = sqlc.arg('changed_at')
WHERE id = sqlc.arg('id') AND company_id = sqlc.arg('company_id')
RETURNING *;

-- name: CreateProvisioningUser :one
INSERT INTO users (
    id, company_id, email, first_name, last_name, role, status,
    source, external_id
)
VALUES (
    sqlc.arg('id'), sqlc.arg('company_id'), sqlc.arg('email'),
    sqlc.arg('first_name'), sqlc.narg('last_name'), sqlc.arg('role'),
    'invited', sqlc.arg('source'), sqlc.narg('external_id')
)
RETURNING *;

-- name: CreateUserExternalIdentity :one
INSERT INTO user_external_identities (
    id, company_id, integration_id, user_id, provider, external_account_id,
    external_user_id, status, last_verified_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8)
RETURNING *;

-- name: GetProvisioningUsers :many
SELECT u.*, identity.id AS external_identity_id, identity.external_user_id
FROM users AS u
JOIN user_external_identities AS identity
  ON identity.company_id = u.company_id
 AND identity.user_id = u.id
JOIN company_integrations AS integration
  ON integration.company_id = identity.company_id
 AND integration.id = identity.integration_id
WHERE integration.provider = sqlc.arg('provider')
  AND integration.external_account_id = sqlc.arg('external_account_id')
  AND u.role IN ('owner', 'admin')
ORDER BY CASE u.role WHEN 'owner' THEN 0 ELSE 1 END, u.id;

-- name: GetExternalIdentityForSSO :one
SELECT sqlc.embed(identity), sqlc.embed(integration), sqlc.embed(u),
       company.status AS company_status
FROM user_external_identities AS identity
JOIN company_integrations AS integration
  ON integration.company_id = identity.company_id
 AND integration.id = identity.integration_id
JOIN users AS u
  ON u.company_id = identity.company_id
 AND u.id = identity.user_id
JOIN companies AS company ON company.id = identity.company_id
WHERE integration.provider = sqlc.arg('provider')
  AND integration.external_account_id = sqlc.arg('external_account_id')
  AND identity.external_user_id = sqlc.arg('external_user_id')
FOR UPDATE OF identity, integration, u, company;

-- name: MarkExternalIdentityVerified :one
UPDATE user_external_identities
SET last_verified_at = sqlc.arg('verified_at'),
    updated_at = sqlc.arg('verified_at')
WHERE company_id = sqlc.arg('company_id')
  AND id = sqlc.arg('id')
RETURNING *;

-- name: GetProvisioningRequestForUpdate :one
SELECT *
FROM provisioning_requests
WHERE provider = sqlc.arg('provider')
  AND idempotency_key = sqlc.arg('idempotency_key')
FOR UPDATE;

-- name: CreateProvisioningRequest :one
INSERT INTO provisioning_requests (
    provider, idempotency_key, request_hash, external_account_id,
    company_id, integration_id, initiator_user_id, created_at, expires_at
)
VALUES (
    sqlc.arg('provider'), sqlc.arg('idempotency_key'), sqlc.arg('request_hash'),
    sqlc.arg('external_account_id'), sqlc.arg('company_id'),
    sqlc.arg('integration_id'), sqlc.arg('initiator_user_id'),
    sqlc.arg('created_at'), sqlc.arg('expires_at')
)
RETURNING *;

-- name: DeleteExpiredProvisioningRequests :execrows
DELETE FROM provisioning_requests
WHERE expires_at < sqlc.arg('before');

-- name: RevokeBootstrapActivations :execrows
UPDATE bootstrap_activations
SET revoked_at = sqlc.arg('revoked_at'),
    revocation_reason = sqlc.arg('revocation_reason')
WHERE company_id = sqlc.arg('company_id')
  AND user_id = sqlc.arg('user_id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL;

-- name: CreateBootstrapActivation :one
INSERT INTO bootstrap_activations (
    id, company_id, user_id, role, purpose, token_hash, expires_at, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetBootstrapActivation :one
SELECT sqlc.embed(activation), sqlc.embed(u), c.name AS company_name,
       c.status AS company_status, integration.status AS integration_status,
       identity.status AS external_identity_status
FROM bootstrap_activations AS activation
JOIN users AS u
  ON u.company_id = activation.company_id
 AND u.id = activation.user_id
JOIN companies AS c ON c.id = activation.company_id
JOIN user_external_identities AS identity
  ON identity.company_id = activation.company_id
 AND identity.user_id = activation.user_id
JOIN company_integrations AS integration
  ON integration.company_id = identity.company_id
 AND integration.id = identity.integration_id
WHERE activation.token_hash = sqlc.arg('token_hash');

-- name: GetBootstrapActivationForUpdate :one
SELECT sqlc.embed(activation), sqlc.embed(u), c.name AS company_name,
       c.status AS company_status, integration.status AS integration_status,
       identity.status AS external_identity_status
FROM bootstrap_activations AS activation
JOIN users AS u
  ON u.company_id = activation.company_id
 AND u.id = activation.user_id
JOIN companies AS c ON c.id = activation.company_id
JOIN user_external_identities AS identity
  ON identity.company_id = activation.company_id
 AND identity.user_id = activation.user_id
JOIN company_integrations AS integration
  ON integration.company_id = identity.company_id
 AND integration.id = identity.integration_id
WHERE activation.token_hash = sqlc.arg('token_hash')
FOR UPDATE OF activation, u, c, identity, integration;

-- name: GetOpenBootstrapActivationForUser :one
SELECT *
FROM bootstrap_activations
WHERE company_id = sqlc.arg('company_id')
  AND user_id = sqlc.arg('user_id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: ConsumeBootstrapActivation :one
UPDATE bootstrap_activations
SET consumed_at = sqlc.arg('consumed_at')
WHERE id = sqlc.arg('id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg('consumed_at')
RETURNING *;

-- name: ActivateBootstrapUser :one
UPDATE users
SET status = 'active', updated_at = sqlc.arg('activated_at')
WHERE company_id = sqlc.arg('company_id')
  AND id = sqlc.arg('user_id')
  AND role = sqlc.arg('role')
  AND status = 'invited'
  AND external_deleted_at IS NULL
RETURNING *;

-- name: GetPendingOnboardingUser :one
SELECT *
FROM users
WHERE company_id = sqlc.arg('company_id')
  AND id <> sqlc.arg('activated_user_id')
  AND source = 'external'
  AND role IN ('owner', 'admin')
  AND status = 'invited'
  AND external_deleted_at IS NULL
ORDER BY CASE role WHEN 'owner' THEN 0 ELSE 1 END, created_at, id
LIMIT 1
FOR UPDATE;

-- name: GetPendingOnboardingUserForActor :one
SELECT pending.*
FROM users AS pending
WHERE pending.company_id = sqlc.arg('company_id')
  AND pending.id <> sqlc.arg('actor_user_id')
  AND pending.source = 'external'
  AND pending.role IN ('owner', 'admin')
  AND pending.status = 'invited'
  AND pending.external_deleted_at IS NULL
  AND EXISTS (
      SELECT 1 FROM bootstrap_activations AS activation
      WHERE activation.company_id = pending.company_id
        AND activation.user_id = pending.id
  )
ORDER BY CASE pending.role WHEN 'owner' THEN 0 ELSE 1 END, pending.created_at, pending.id
LIMIT 1
FOR UPDATE;

-- name: ListOnboardingParticipants :many
SELECT u.*,
       current_activation.expires_at IS NOT NULL AS has_open_activation,
       COALESCE(current_activation.expires_at, 'epoch'::timestamptz) AS activation_expires_at
FROM users AS u
JOIN user_external_identities AS identity
  ON identity.company_id = u.company_id AND identity.user_id = u.id
LEFT JOIN LATERAL (
    SELECT activation.expires_at
    FROM bootstrap_activations AS activation
    WHERE activation.company_id = u.company_id
      AND activation.user_id = u.id
      AND activation.consumed_at IS NULL
      AND activation.revoked_at IS NULL
    ORDER BY activation.created_at DESC, activation.id DESC
    LIMIT 1
) AS current_activation ON true
WHERE u.company_id = sqlc.arg('company_id')
  AND u.source = 'external'
  AND u.role IN ('owner', 'admin')
ORDER BY CASE u.role WHEN 'owner' THEN 0 ELSE 1 END, u.id;

-- name: GetCompanyForOnboardingUpdate :one
SELECT *
FROM companies
WHERE id = sqlc.arg('company_id')
FOR UPDATE;

-- name: GetOnboardingIntegration :one
SELECT *
FROM company_integrations
WHERE company_id = sqlc.arg('company_id')
ORDER BY created_at, id
LIMIT 1;

-- name: GetOnboardingIntegrationForUpdate :one
SELECT *
FROM company_integrations
WHERE company_id = sqlc.arg('company_id')
ORDER BY created_at, id
LIMIT 1
FOR UPDATE;

-- name: IsPendingBootstrapUser :one
SELECT EXISTS (
    SELECT 1
    FROM bootstrap_activations AS activation
    WHERE activation.company_id = sqlc.arg('company_id')
      AND activation.user_id = sqlc.arg('user_id')
      AND activation.consumed_at IS NULL
      AND activation.revoked_at IS NULL
);

-- name: CompleteCompanyOnboarding :one
UPDATE companies AS company
SET status = 'active', onboarding_completed_at = sqlc.arg('completed_at'),
    updated_at = sqlc.arg('completed_at')
WHERE company.id = sqlc.arg('company_id')
  AND company.status = 'onboarding'
  AND EXISTS (
      SELECT 1
      FROM users AS owner_user
      JOIN credentials AS owner_credential
        ON owner_credential.company_id = owner_user.company_id
       AND owner_credential.user_id = owner_user.id
      WHERE owner_user.company_id = company.id
        AND owner_user.id = company.owner_id
        AND owner_user.role = 'owner'
        AND owner_user.status = 'active'
        AND owner_user.external_deleted_at IS NULL
  )
  AND EXISTS (
      SELECT 1
      FROM users AS admin_user
      JOIN credentials AS admin_credential
        ON admin_credential.company_id = admin_user.company_id
       AND admin_credential.user_id = admin_user.id
      WHERE admin_user.company_id = company.id
        AND admin_user.role = 'admin'
        AND admin_user.status = 'active'
        AND admin_user.external_deleted_at IS NULL
  )
RETURNING *;

-- name: RevokeActiveSSOTokens :execrows
UPDATE sso_tokens
SET revoked_at = sqlc.arg('revoked_at')
WHERE external_identity_id = sqlc.arg('external_identity_id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL;

-- name: CreateSSOToken :one
INSERT INTO sso_tokens (
    id, company_id, user_id, external_identity_id, token_hash, expires_at, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetSSOTokenByHashForUpdate :one
SELECT token.id AS sso_token_id,
       token.company_id,
       token.user_id,
       token.external_identity_id,
       token.expires_at,
       token.consumed_at,
       token.revoked_at,
       sqlc.embed(u),
       identity.status AS external_identity_status,
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
WHERE token.token_hash = sqlc.arg('token_hash')
FOR UPDATE OF token, identity, integration, u, company;

-- name: ConsumeSSOToken :one
UPDATE sso_tokens
SET consumed_at = sqlc.arg('consumed_at')
WHERE id = sqlc.arg('id')
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg('consumed_at')
RETURNING *;

-- name: DeleteExpiredBootstrapActivations :execrows
DELETE FROM bootstrap_activations
WHERE expires_at < sqlc.arg('before');

-- name: DeleteExpiredSSOTokens :execrows
DELETE FROM sso_tokens
WHERE expires_at < sqlc.arg('before');
