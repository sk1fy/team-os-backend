-- name: GetCourses :many
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at
FROM courses
WHERE company_id = $1
ORDER BY created_at, id;

-- name: GetCoursesByIds :many
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at
FROM courses
WHERE company_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY created_at, id;

-- name: GetCourse :one
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at
FROM courses
WHERE company_id = $1 AND id = $2;

-- name: CreateCourse :one
INSERT INTO courses (
    id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at;

-- name: UpdateCourse :one
UPDATE courses
SET title = coalesce(sqlc.narg(title), title),
    description = CASE WHEN sqlc.arg(set_description)::boolean THEN sqlc.narg(description) ELSE description END,
    status = coalesce(sqlc.narg(status), status),
    sequential = coalesce(sqlc.narg(sequential), sequential),
    deadline_days = CASE WHEN sqlc.arg(set_deadline_days)::boolean THEN sqlc.narg(deadline_days) ELSE deadline_days END,
    updated_at = sqlc.arg(updated_at)
WHERE company_id = $1 AND id = $2
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at;

-- name: DeleteCourse :execrows
DELETE FROM courses
WHERE company_id = $1 AND id = $2;
