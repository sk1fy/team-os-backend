-- name: ListBoards :many
SELECT id, company_id, name, type, department_id, owner_id, created_at
FROM boards
WHERE company_id = $1
ORDER BY created_at;

-- name: GetBoard :one
SELECT id, company_id, name, type, department_id, owner_id, created_at
FROM boards
WHERE company_id = $1 AND id = $2;