-- name: ListDepartments :many
SELECT * FROM departments
WHERE company_id = $1
ORDER BY parent_id NULLS FIRST, "order", name;

-- name: GetDepartment :one
SELECT * FROM departments WHERE company_id = $1 AND id = $2;

-- name: CreateDepartment :one
INSERT INTO departments (
    id, company_id, name, parent_id, head_user_id, valuable_final_product, "order"
)
VALUES (
    $1, $2, $3, $4, $5, $6,
    (SELECT count(*) FROM departments WHERE company_id = $2 AND parent_id IS NOT DISTINCT FROM $4)
)
RETURNING *;

-- name: UpdateDepartment :one
UPDATE departments
SET name = COALESCE(sqlc.narg('name'), name),
    head_user_id = CASE WHEN sqlc.arg('set_head')::boolean THEN sqlc.narg('head_user_id') ELSE head_user_id END,
    valuable_final_product = CASE WHEN sqlc.arg('set_vfp')::boolean THEN sqlc.narg('valuable_final_product') ELSE valuable_final_product END,
    updated_at = now()
WHERE company_id = sqlc.arg('company_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: CountDepartmentChildren :one
SELECT count(*) FROM departments WHERE company_id = $1 AND parent_id = $2;

-- name: CountDepartmentPositions :one
SELECT count(*) FROM positions WHERE company_id = $1 AND department_id = $2;

-- name: DeleteDepartment :execrows
DELETE FROM departments WHERE company_id = $1 AND id = $2;

-- name: MoveDepartment :one
UPDATE departments AS moving
SET parent_id = $3,
    "order" = (
        SELECT count(*)
        FROM departments AS sibling
        WHERE sibling.company_id = $1
          AND sibling.parent_id IS NOT DISTINCT FROM $3
          AND sibling.id <> $2
    ),
    updated_at = now()
WHERE moving.company_id = $1 AND moving.id = $2
RETURNING moving.*;

-- name: ListPositions :many
SELECT * FROM positions WHERE company_id = $1 ORDER BY name;

-- name: GetPosition :one
SELECT * FROM positions WHERE company_id = $1 AND id = $2;

-- name: CreatePosition :one
INSERT INTO positions (id, company_id, name, department_id, level, description)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdatePosition :one
UPDATE positions
SET name = COALESCE(sqlc.narg('name'), name),
    department_id = COALESCE(sqlc.narg('department_id'), department_id),
    level = COALESCE(sqlc.narg('level'), level),
    description = CASE WHEN sqlc.arg('set_description')::boolean THEN sqlc.narg('description') ELSE description END,
    updated_at = now()
WHERE company_id = sqlc.arg('company_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: DeletePosition :execrows
DELETE FROM positions WHERE company_id = $1 AND id = $2;

-- name: GetPositionUserIDs :many
SELECT up.user_id
FROM user_positions up
JOIN users u ON u.id = up.user_id AND u.company_id = up.company_id
WHERE up.company_id = $1 AND up.position_id = $2 AND u.status = 'active'
ORDER BY up.user_id;

-- name: ListUsers :many
SELECT u.*,
       COALESCE(array_agg(up.position_id) FILTER (WHERE up.position_id IS NOT NULL), '{}')::uuid[] AS position_ids,
       CASE
           WHEN EXISTS (SELECT 1 FROM access_links access WHERE access.company_id = u.company_id AND access.user_id = u.id) THEN 'link'
           WHEN EXISTS (SELECT 1 FROM credentials credential WHERE credential.company_id = u.company_id AND credential.user_id = u.id) THEN 'password'
           ELSE 'none'
       END::text AS access_mode
FROM users u
LEFT JOIN user_positions up ON up.user_id = u.id
WHERE u.company_id = $1
GROUP BY u.id
ORDER BY u.created_at, u.id;

-- name: GetUserWithPositions :one
SELECT u.*,
       COALESCE(array_agg(up.position_id) FILTER (WHERE up.position_id IS NOT NULL), '{}')::uuid[] AS position_ids,
       CASE
           WHEN EXISTS (SELECT 1 FROM access_links access WHERE access.company_id = u.company_id AND access.user_id = u.id) THEN 'link'
           WHEN EXISTS (SELECT 1 FROM credentials credential WHERE credential.company_id = u.company_id AND credential.user_id = u.id) THEN 'password'
           ELSE 'none'
       END::text AS access_mode
FROM users u
LEFT JOIN user_positions up ON up.user_id = u.id
WHERE u.company_id = $1 AND u.id = $2
GROUP BY u.id;

-- name: LockAmoUserSync :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('company_id')::uuid::text, 0));

-- name: FindUserForAmoSync :one
SELECT *
FROM users
WHERE company_id = sqlc.arg('company_id')
  AND (external_id = sqlc.arg('external_id') OR email = sqlc.arg('email'))
ORDER BY (external_id = sqlc.arg('external_id')) DESC
LIMIT 1;

-- name: CreateAmoUser :one
INSERT INTO users (
    id, company_id, email, first_name, last_name, avatar_url, avatar_source,
    role, status, source, external_id, external_group_id, external_group_name
)
VALUES ($1, $2, $3, $4, $5, $6, sqlc.narg('avatar_source'),
    'employee', 'active', 'amo', $7, $8, $9)
RETURNING *;

-- name: UpdateUser :one
UPDATE users
SET first_name = COALESCE(sqlc.narg('first_name'), first_name),
    last_name = COALESCE(sqlc.narg('last_name'), last_name),
    phone = CASE WHEN sqlc.arg('set_phone')::boolean THEN sqlc.narg('phone') ELSE phone END,
    birth_date = CASE WHEN sqlc.arg('set_birth_date')::boolean THEN sqlc.narg('birth_date') ELSE birth_date END,
    hired_at = CASE WHEN sqlc.arg('set_hired_at')::boolean THEN sqlc.narg('hired_at') ELSE hired_at END,
    vacation_allowance = CASE WHEN sqlc.arg('set_vacation')::boolean THEN sqlc.narg('vacation_allowance') ELSE vacation_allowance END,
    role = COALESCE(sqlc.narg('role'), role),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = now()
WHERE company_id = sqlc.arg('company_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: ReassignUserInvites :exec
UPDATE invites
SET invited_by_id = sqlc.arg('replacement_user_id'), updated_at = now()
WHERE company_id = sqlc.arg('company_id') AND invited_by_id = sqlc.arg('deleted_user_id');

-- name: DeleteLocalUser :execrows
DELETE FROM users
WHERE company_id = sqlc.arg('company_id')
  AND id = sqlc.arg('id')
  AND source = 'local';

-- name: DeleteUserPositions :exec
DELETE FROM user_positions WHERE company_id = $1 AND user_id = $2;

-- name: AssignUserPosition :exec
INSERT INTO user_positions (company_id, user_id, position_id)
VALUES ($1, $2, $3);

-- name: ListInvites :many
SELECT * FROM invites WHERE company_id = $1 ORDER BY created_at DESC;

-- name: CreateInvite :one
INSERT INTO invites (
    id, company_id, email, token, role, position_id, department_id, invited_by_id, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetInvite :one
SELECT * FROM invites WHERE company_id = $1 AND id = $2;

-- name: ResendInvite :one
UPDATE invites
SET created_at = $3, expires_at = $4, updated_at = $3
WHERE company_id = $1 AND id = $2 AND status = 'pending'
RETURNING *;

-- name: RevokeInvite :one
UPDATE invites
SET status = 'expired', updated_at = now()
WHERE company_id = $1 AND id = $2 AND status <> 'accepted'
RETURNING *;
