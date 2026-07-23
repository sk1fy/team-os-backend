-- name: GetCourses :many
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id
FROM courses
WHERE company_id = $1 AND lifecycle_status <> 'deleted'
ORDER BY created_at, id;

-- name: GetCoursesFiltered :many
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id
FROM courses
WHERE company_id = sqlc.arg(company_id)
  AND (sqlc.narg(owner_type)::text IS NULL OR owner_type = sqlc.narg(owner_type))
  AND (sqlc.narg(owner_user_id)::uuid IS NULL OR owner_user_id = sqlc.narg(owner_user_id))
  AND (sqlc.narg(lifecycle_status)::text IS NULL OR lifecycle_status = sqlc.narg(lifecycle_status))
  AND (sqlc.narg(distribution_status)::text IS NULL OR distribution_status = sqlc.narg(distribution_status))
ORDER BY created_at, id;

-- name: GetCoursesByIds :many
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id
FROM courses
WHERE company_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY created_at, id;

-- name: GetCourse :one
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id
FROM courses
WHERE company_id = $1 AND id = $2;

-- name: GetCourseForUpdate :one
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id
FROM courses
WHERE company_id = $1 AND id = $2
FOR UPDATE;

-- name: LockCourseOrder :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(course_id)::uuid::text, 0));

-- name: CreateCourse :one
INSERT INTO courses (
    id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11)
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id;

-- name: CreateOwnedCourse :one
INSERT INTO courses (
    id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id
)
VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(title), sqlc.narg(description),
    sqlc.narg(cover_url), sqlc.arg(status), sqlc.arg(author_id),
    sqlc.arg(sequential), sqlc.narg(deadline_days), sqlc.arg(created_at),
    sqlc.arg(created_at), sqlc.arg(visibility), sqlc.arg(owner_type),
    sqlc.narg(owner_user_id), sqlc.arg(created_by_id)
)
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id;

-- name: UpdateCourse :one
UPDATE courses
SET title = coalesce(sqlc.narg(title), title),
    description = CASE WHEN sqlc.arg(set_description)::boolean THEN sqlc.narg(description) ELSE description END,
    status = coalesce(sqlc.narg(status), status),
    sequential = coalesce(sqlc.narg(sequential), sequential),
    deadline_days = CASE WHEN sqlc.arg(set_deadline_days)::boolean THEN sqlc.narg(deadline_days) ELSE deadline_days END,
    visibility = coalesce(sqlc.narg(visibility), visibility),
    updated_at = sqlc.arg(updated_at)
WHERE company_id = $1 AND id = $2 AND lifecycle_status <> 'deleted'
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id;

-- name: GetPublicCourse :one
SELECT id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id
FROM courses
WHERE id = $1
  AND status = 'published'
  AND visibility = 'public'
  AND lifecycle_status = 'active'
  AND distribution_status <> 'blocked';

-- name: ArchiveCourse :one
UPDATE courses
SET lifecycle_status = 'archived',
    archived_at = sqlc.arg(archived_at),
    archived_by_id = sqlc.arg(archived_by_id),
    updated_at = sqlc.arg(archived_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND lifecycle_status = 'active'
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id;

-- name: RestoreCourse :one
UPDATE courses
SET lifecycle_status = 'active',
    archived_at = NULL,
    archived_by_id = NULL,
    updated_at = sqlc.arg(restored_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND lifecycle_status = 'archived'
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id;

-- name: SoftDeleteCourse :one
UPDATE courses
SET lifecycle_status = 'deleted',
    deleted_at = sqlc.arg(deleted_at),
    deleted_by_id = sqlc.arg(deleted_by_id),
    updated_at = sqlc.arg(deleted_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND lifecycle_status <> 'deleted'
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id;

-- name: UpdateCourseDistributionStatus :one
UPDATE courses
SET distribution_status = sqlc.arg(distribution_status),
    updated_at = sqlc.arg(updated_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND owner_type = 'partner'
  AND lifecycle_status <> 'deleted'
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id;

-- name: DeleteCourseLegacyHard :execrows
DELETE FROM courses
WHERE company_id = $1 AND id = $2;
