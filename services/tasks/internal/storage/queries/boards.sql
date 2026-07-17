-- name: ListBoards :many
SELECT id, company_id, name, type, department_id, owner_id, created_at
FROM boards
WHERE company_id = $1
ORDER BY created_at;

-- name: LockCompanyBoardBootstrap :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('company_id')::uuid::text, 1));

-- name: CreateBoard :one
INSERT INTO boards (id, company_id, name, type, department_id, owner_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, company_id, name, type, department_id, owner_id, created_at;

-- name: GetBoard :one
SELECT id, company_id, name, type, department_id, owner_id, created_at
FROM boards
WHERE company_id = $1 AND id = $2;
