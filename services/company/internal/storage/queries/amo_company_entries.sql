-- name: GetAmoEntryCompanyStatus :many
SELECT company.id, company.status,
       CASE WHEN (company.amo_account_id IS NOT NULL
                  AND company.amo_account_id <> sqlc.arg('account_id'))
                  OR (integration.external_account_id IS NOT NULL
                  AND integration.external_account_id <> sqlc.arg('account_id'))
            THEN 'suspended' ELSE COALESCE(integration.status, 'active') END::text AS integration_status
FROM companies AS company
LEFT JOIN company_integrations AS integration
  ON integration.company_id = company.id AND integration.provider = 'rakurs'
WHERE company.amo_account_id = sqlc.arg('account_id')
   OR integration.external_account_id = sqlc.arg('account_id');

-- name: FindAmoEntryLegacyUserForUpdate :one
SELECT * FROM users
WHERE company_id = sqlc.arg('company_id')
  AND source = 'amo' AND external_id = sqlc.arg('actor_id')
FOR UPDATE;

-- name: TouchActiveAmoEntryIdentity :one
UPDATE user_external_identities
SET last_verified_at = sqlc.arg('verified_at'), updated_at = sqlc.arg('verified_at')
WHERE company_id = sqlc.arg('company_id') AND id = sqlc.arg('identity_id') AND status = 'active'
RETURNING *;

-- name: CreateAmoCompanyEntry :one
INSERT INTO amo_company_entries (
    id, company_id, user_id, external_identity_id, account_id, actor_id,
    core_integration_id, core_installation_id, token_hash, created_at, expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: GetAmoCompanyEntry :one
SELECT * FROM amo_company_entries WHERE token_hash = $1;

-- name: GetAmoCompanyEntryForUpdate :one
SELECT sqlc.embed(entry), sqlc.embed(u),
       identity.status AS identity_status,
       identity.provider AS identity_provider,
       identity.external_account_id AS identity_account_id,
       identity.external_user_id AS identity_actor_id,
       integration.external_account_id AS integration_account_id,
       integration.status AS integration_status,
       integration.provider,
       company.status AS company_status,
       company.amo_account_id AS company_account_id
FROM amo_company_entries AS entry
JOIN user_external_identities AS identity
  ON identity.company_id = entry.company_id AND identity.user_id = entry.user_id
 AND identity.id = entry.external_identity_id
JOIN company_integrations AS integration
  ON integration.company_id = identity.company_id AND integration.id = identity.integration_id
JOIN users AS u ON u.company_id = entry.company_id AND u.id = entry.user_id
JOIN companies AS company ON company.id = entry.company_id
WHERE entry.token_hash = $1
FOR UPDATE OF entry, identity, integration, u, company;

-- name: ConsumeAmoCompanyEntry :execrows
UPDATE amo_company_entries SET consumed_at = sqlc.arg('now')
WHERE id = sqlc.arg('id') AND consumed_at IS NULL AND expires_at > sqlc.arg('now');

-- name: RevokeAmoEntryIdentities :exec
UPDATE user_external_identities SET status = 'deactivated', updated_at = sqlc.arg('revoked_at')
WHERE company_id = sqlc.arg('company_id') AND user_id = sqlc.arg('user_id') AND provider = 'rakurs';
