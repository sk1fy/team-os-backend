-- name: CreateFile :one
INSERT INTO files (id, company_id, uploaded_by, object_key, name, content_type, size, purpose)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetFile :one
SELECT * FROM files WHERE id = $1 AND company_id = $2;

-- name: DeleteFile :one
DELETE FROM files WHERE id = $1 AND company_id = $2 RETURNING object_key;

-- name: CreateCloneOperation :one
INSERT INTO file_clone_operations (
  id, company_id, idempotency_key, requested_by, target_owner_type,
  target_owner_id, source_file_ids, state
) VALUES (
  sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(idempotency_key),
  sqlc.arg(requested_by), sqlc.arg(target_owner_type),
  sqlc.arg(target_owner_id), sqlc.arg(source_file_ids)::uuid[], 'pending'
)
ON CONFLICT (company_id, idempotency_key) DO NOTHING
RETURNING *;

-- name: GetCloneOperationByKey :one
SELECT * FROM file_clone_operations
WHERE company_id = sqlc.arg(company_id) AND idempotency_key = sqlc.arg(idempotency_key);

-- name: GetCloneOperation :one
SELECT * FROM file_clone_operations
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id);

-- name: StartCloneOperation :one
UPDATE file_clone_operations
SET state = 'in_progress', error_message = NULL, updated_at = now()
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND state IN ('pending', 'failed')
RETURNING *;

-- name: FailCloneOperation :one
UPDATE file_clone_operations
SET state = 'failed', error_message = sqlc.arg(error_message), updated_at = now()
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: CompleteCloneOperation :one
UPDATE file_clone_operations
SET state = 'succeeded', error_message = NULL, updated_at = now()
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: CreateFileClone :exec
INSERT INTO file_clones (operation_id, ordinal, source_file_id, target_file_id)
VALUES (sqlc.arg(operation_id), sqlc.arg(ordinal), sqlc.arg(source_file_id), sqlc.arg(target_file_id))
ON CONFLICT (operation_id, source_file_id) DO NOTHING;

-- name: ListFileClones :many
SELECT clone.operation_id, clone.ordinal, clone.source_file_id, clone.target_file_id,
       file.company_id, file.uploaded_by, file.object_key, file.name,
       file.content_type, file.size, file.purpose, file.created_at
FROM file_clones AS clone
JOIN files AS file ON file.id = clone.target_file_id
WHERE clone.operation_id = sqlc.arg(operation_id)
ORDER BY clone.ordinal;
