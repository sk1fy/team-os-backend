-- name: CreateFile :one
INSERT INTO files (id, company_id, uploaded_by, object_key, name, content_type, size, purpose)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetFile :one
SELECT * FROM files WHERE id = $1 AND company_id = $2;

-- name: DeleteFile :one
DELETE FROM files WHERE id = $1 AND company_id = $2 RETURNING object_key;
