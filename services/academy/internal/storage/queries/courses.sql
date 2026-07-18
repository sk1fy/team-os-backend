-- name: GetCourses :many
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility
FROM courses
WHERE company_id = $1
ORDER BY created_at, id;

-- name: GetCoursesByIds :many
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility
FROM courses
WHERE company_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY created_at, id;

-- name: GetCourse :one
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility
FROM courses
WHERE company_id = $1 AND id = $2;

-- name: LockCourseOrder :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(course_id)::uuid::text, 0));

-- name: CreateCourse :one
INSERT INTO courses (
    id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11)
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility;

-- name: UpdateCourse :one
UPDATE courses
SET title = coalesce(sqlc.narg(title), title),
    description = CASE WHEN sqlc.arg(set_description)::boolean THEN sqlc.narg(description) ELSE description END,
    status = coalesce(sqlc.narg(status), status),
    sequential = coalesce(sqlc.narg(sequential), sequential),
    deadline_days = CASE WHEN sqlc.arg(set_deadline_days)::boolean THEN sqlc.narg(deadline_days) ELSE deadline_days END,
    visibility = coalesce(sqlc.narg(visibility), visibility),
    updated_at = sqlc.arg(updated_at)
WHERE company_id = $1 AND id = $2
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility;

-- name: GetPublicCourse :one
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility
FROM courses
WHERE id = $1 AND status = 'published' AND visibility = 'public';

-- name: DeleteCourse :execrows
DELETE FROM courses
WHERE company_id = $1 AND id = $2;
