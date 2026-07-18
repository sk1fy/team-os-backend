-- name: CreateCompany :one
INSERT INTO companies (id, name, logo_url)
VALUES ($1, $2, $3)
RETURNING *;

-- name: SetCompanyOwner :one
UPDATE companies
SET owner_id = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetCompany :one
SELECT * FROM companies WHERE id = $1;

-- name: UpdateCompany :one
UPDATE companies
SET name = COALESCE(sqlc.narg('name'), name),
    logo_url = CASE WHEN sqlc.arg('set_logo')::boolean THEN sqlc.narg('logo_url') ELSE logo_url END,
    amo_account_id = CASE WHEN sqlc.arg('set_amo_account_id')::boolean THEN sqlc.narg('amo_account_id') ELSE amo_account_id END,
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateUser :one
INSERT INTO users (
    id, company_id, email, first_name, last_name, phone, avatar_url,
    role, status, birth_date, hired_at, vacation_allowance
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE company_id = $1 AND id = $2;

-- name: GetUserForAccessUpdate :one
SELECT * FROM users WHERE company_id = $1 AND id = $2 FOR UPDATE;

-- name: GetUserForLogin :one
SELECT sqlc.embed(u), c.password_hash
FROM users u
JOIN credentials c ON c.user_id = u.id
WHERE u.email = $1
FOR SHARE OF u;

-- name: GetUserByEmailForUpdate :one
SELECT * FROM users
WHERE email = $1
FOR UPDATE;

-- name: SetCredential :exec
INSERT INTO credentials (company_id, user_id, password_hash)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE
SET password_hash = EXCLUDED.password_hash, updated_at = now();

-- name: DeleteCredential :exec
DELETE FROM credentials WHERE company_id = $1 AND user_id = $2;

-- name: UpsertAccessLink :one
INSERT INTO access_links (company_id, user_id, token)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE
SET token = EXCLUDED.token, created_at = now(), updated_at = now()
RETURNING *;

-- name: GetAccessLink :one
SELECT * FROM access_links WHERE company_id = $1 AND user_id = $2;

-- name: DeleteAccessLink :exec
DELETE FROM access_links WHERE company_id = $1 AND user_id = $2;

-- name: CreateEmployeeAccessAudit :exec
INSERT INTO employee_access_audit (
    id, company_id, target_user_id, actor_user_id, action, mode, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetUserByAccessToken :one
SELECT u.*
FROM users u
JOIN access_links access ON access.user_id = u.id AND access.company_id = u.company_id
WHERE access.token = $1 AND u.status = 'active'
FOR SHARE OF u, access;

-- name: GetUserAccessMode :one
SELECT CASE
    WHEN EXISTS (
        SELECT 1 FROM access_links access
        WHERE access.company_id = sqlc.arg('company_id') AND access.user_id = sqlc.arg('user_id')
    ) THEN 'link'
    WHEN EXISTS (
        SELECT 1 FROM credentials credential
        WHERE credential.company_id = sqlc.arg('company_id') AND credential.user_id = sqlc.arg('user_id')
    ) THEN 'password'
    ELSE 'none'
END::text AS access_mode;

-- name: GetUserPositionIDs :many
SELECT position_id
FROM user_positions
WHERE company_id = $1 AND user_id = $2;

-- name: GetUserDepartmentClaims :many
WITH RECURSIVE direct_departments AS (
    SELECT DISTINCT p.department_id AS id
    FROM user_positions up
    JOIN positions p ON p.id = up.position_id
    WHERE up.company_id = $1 AND up.user_id = $2
), department_chain AS (
    SELECT d.id, d.parent_id
    FROM departments d
    JOIN direct_departments dd ON dd.id = d.id
    UNION
    SELECT parent.id, parent.parent_id
    FROM departments parent
    JOIN department_chain child ON child.parent_id = parent.id
)
SELECT DISTINCT id FROM department_chain;

-- name: UpdateCurrentUser :one
UPDATE users
SET first_name = COALESCE(sqlc.narg('first_name'), first_name),
    last_name = COALESCE(sqlc.narg('last_name'), last_name),
    phone = CASE WHEN sqlc.arg('set_phone')::boolean THEN sqlc.narg('phone') ELSE phone END,
    updated_at = now()
WHERE company_id = sqlc.arg('company_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: CreateSession :one
INSERT INTO sessions (
    id, company_id, user_id, refresh_hash, expires_at, rotated_from, user_agent, ip_address
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetSessionByHashForUpdate :one
SELECT * FROM sessions
WHERE refresh_hash = $1
FOR UPDATE;

-- name: RotateSession :execrows
UPDATE sessions
SET revoked_at = $2, last_used_at = $2, replaced_by = $3
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSessionByHash :execrows
UPDATE sessions
SET revoked_at = COALESCE(revoked_at, $2), last_used_at = $2
WHERE refresh_hash = $1;

-- name: RevokeAllUserSessions :exec
UPDATE sessions
SET revoked_at = COALESCE(revoked_at, $2)
WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < $1;

-- name: GetInviteByToken :one
SELECT * FROM invites WHERE token = $1;

-- name: GetInviteByTokenForUpdate :one
SELECT * FROM invites WHERE token = $1 FOR UPDATE;

-- name: AcceptInvite :one
UPDATE invites
SET status = 'accepted', updated_at = now()
WHERE id = $1 AND status = 'pending' AND expires_at > now()
RETURNING *;

-- name: ActivateInvitedUser :one
UPDATE users
SET first_name = $2,
    last_name = $3,
    role = $4,
    status = 'active',
    updated_at = now()
WHERE id = $1 AND company_id = $5
RETURNING *;

-- name: CreateOutboxEvent :one
INSERT INTO outbox (id, company_id, subject, payload, headers, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;
