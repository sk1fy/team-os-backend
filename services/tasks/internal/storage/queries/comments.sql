-- name: ListCommentsByTask :many
SELECT id, task_id, author_id, content, created_at
FROM comments
WHERE task_id = $1
ORDER BY created_at;

-- name: CreateComment :one
INSERT INTO comments (id, task_id, author_id, content)
VALUES ($1, $2, $3, $4)
RETURNING id, task_id, author_id, content, created_at;