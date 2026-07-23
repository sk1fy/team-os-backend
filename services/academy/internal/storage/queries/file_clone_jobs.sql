-- name: CreateFileCloneJob :one
INSERT INTO academy_file_clone_jobs (
    id, company_id, operation_type, aggregate_id, idempotency_key,
    source_owner_type, source_owner_id, target_owner_type, target_owner_id,
    status, attempts, next_attempt_at, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(operation_type),
    sqlc.arg(aggregate_id), sqlc.arg(idempotency_key),
    sqlc.arg(source_owner_type), sqlc.arg(source_owner_id),
    sqlc.arg(target_owner_type), sqlc.arg(target_owner_id),
    'pending', 0, sqlc.arg(created_at), sqlc.arg(created_at), sqlc.arg(created_at)
)
ON CONFLICT (company_id, operation_type, idempotency_key) DO NOTHING
RETURNING id, company_id, operation_type, aggregate_id, idempotency_key,
    source_owner_type, source_owner_id, target_owner_type, target_owner_id,
    status, attempts, next_attempt_at, last_error, created_at, updated_at,
    completed_at;

-- name: ListCourseVersionFileIDsForClone :many
SELECT file_id::uuid
FROM (
    SELECT version.cover_file_id AS file_id
    FROM course_versions AS version
    WHERE version.company_id = sqlc.arg(company_id)
      AND version.id = sqlc.arg(course_version_id)
      AND version.cover_file_id IS NOT NULL
    UNION
    SELECT unnest(lesson.file_ids) AS file_id
    FROM course_version_lessons AS lesson
    WHERE lesson.company_id = sqlc.arg(company_id)
      AND lesson.course_version_id = sqlc.arg(course_version_id)
) AS files
ORDER BY file_id;

-- name: GetFileCloneJobByIdempotencyKey :one
SELECT id, company_id, operation_type, aggregate_id, idempotency_key,
    source_owner_type, source_owner_id, target_owner_type, target_owner_id,
    status, attempts, next_attempt_at, last_error, created_at, updated_at,
    completed_at
FROM academy_file_clone_jobs
WHERE company_id = sqlc.arg(company_id)
  AND operation_type = sqlc.arg(operation_type)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: LockCourseVersionFileCloneJobs :many
SELECT id, status
FROM academy_file_clone_jobs
WHERE company_id = sqlc.arg(company_id)
  AND target_owner_type = 'course_version'
  AND target_owner_id = sqlc.arg(course_version_id)
ORDER BY id
FOR UPDATE;

-- name: AddFileCloneJobItems :many
INSERT INTO academy_file_clone_job_items (
    id, company_id, job_id, source_file_id, status, attempts, updated_at
)
SELECT gen_random_uuid(), job.company_id, job.id, source_file_id,
    'pending', 0, sqlc.arg(updated_at)
FROM academy_file_clone_jobs AS job
CROSS JOIN unnest(sqlc.arg(source_file_ids)::uuid[]) AS source_file_id
WHERE job.company_id = sqlc.arg(company_id)
  AND job.id = sqlc.arg(job_id)
ON CONFLICT (job_id, source_file_id) DO UPDATE
SET updated_at = academy_file_clone_job_items.updated_at
RETURNING id, company_id, job_id, source_file_id, target_file_id,
    status, attempts, last_error, updated_at;

-- name: ClaimFileCloneJobs :many
WITH candidates AS (
    SELECT id
    FROM academy_file_clone_jobs
    WHERE status IN ('pending', 'failed')
      AND next_attempt_at <= sqlc.arg(claimed_at)
    ORDER BY next_attempt_at, created_at, id
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
)
UPDATE academy_file_clone_jobs AS job
SET status = 'running', attempts = job.attempts + 1,
    last_error = NULL, updated_at = sqlc.arg(claimed_at)
FROM candidates
WHERE job.id = candidates.id
RETURNING job.id, job.company_id, job.operation_type, job.aggregate_id,
    job.idempotency_key, job.source_owner_type, job.source_owner_id,
    job.target_owner_type, job.target_owner_id, job.status, job.attempts,
    job.next_attempt_at, job.last_error, job.created_at, job.updated_at,
    job.completed_at;

-- name: RequeueStaleFileCloneJobs :execrows
UPDATE academy_file_clone_jobs
SET status = 'failed', last_error = sqlc.arg(last_error),
    next_attempt_at = sqlc.arg(next_attempt_at),
    updated_at = sqlc.arg(updated_at), completed_at = NULL
WHERE status = 'running' AND updated_at < sqlc.arg(stale_before);

-- name: ListFileCloneJobItems :many
SELECT id, company_id, job_id, source_file_id, target_file_id,
    status, attempts, last_error, updated_at
FROM academy_file_clone_job_items
WHERE company_id = sqlc.arg(company_id) AND job_id = sqlc.arg(job_id)
ORDER BY id;

-- name: CompleteFileCloneJobItem :one
UPDATE academy_file_clone_job_items
SET target_file_id = sqlc.arg(target_file_id), status = 'completed',
    attempts = attempts + 1, last_error = NULL, updated_at = sqlc.arg(updated_at)
WHERE company_id = sqlc.arg(company_id)
  AND job_id = sqlc.arg(job_id)
  AND source_file_id = sqlc.arg(source_file_id)
  AND status <> 'completed'
RETURNING id, company_id, job_id, source_file_id, target_file_id,
    status, attempts, last_error, updated_at;

-- name: FailFileCloneJobItem :one
UPDATE academy_file_clone_job_items
SET status = 'failed', attempts = attempts + 1,
    last_error = sqlc.arg(last_error), updated_at = sqlc.arg(updated_at)
WHERE company_id = sqlc.arg(company_id)
  AND job_id = sqlc.arg(job_id)
  AND source_file_id = sqlc.arg(source_file_id)
  AND status <> 'completed'
RETURNING id, company_id, job_id, source_file_id, target_file_id,
    status, attempts, last_error, updated_at;

-- name: CompleteFileCloneJob :one
UPDATE academy_file_clone_jobs AS job
SET status = 'completed', last_error = NULL,
    updated_at = sqlc.arg(completed_at), completed_at = sqlc.arg(completed_at)
WHERE job.company_id = sqlc.arg(company_id)
  AND job.id = sqlc.arg(id)
  AND job.status = 'running'
  AND NOT EXISTS (
      SELECT 1 FROM academy_file_clone_job_items AS item
      WHERE item.company_id = job.company_id AND item.job_id = job.id
        AND item.status <> 'completed'
  )
RETURNING id, company_id, operation_type, aggregate_id, idempotency_key,
    source_owner_type, source_owner_id, target_owner_type, target_owner_id,
    status, attempts, next_attempt_at, last_error, created_at, updated_at,
    completed_at;

-- name: RetryFileCloneJob :one
UPDATE academy_file_clone_jobs
SET status = 'failed', last_error = sqlc.arg(last_error),
    next_attempt_at = sqlc.arg(next_attempt_at),
    updated_at = sqlc.arg(updated_at), completed_at = NULL
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status = 'running'
RETURNING id, company_id, operation_type, aggregate_id, idempotency_key,
    source_owner_type, source_owner_id, target_owner_type, target_owner_id,
    status, attempts, next_attempt_at, last_error, created_at, updated_at,
    completed_at;

-- name: SetCourseVersionLessonClonedFiles :execrows
UPDATE course_version_lessons AS lesson
SET content = sqlc.arg(content), file_ids = sqlc.arg(file_ids)
FROM course_versions AS version
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.id = sqlc.arg(lesson_id)
  AND version.company_id = lesson.company_id
  AND version.id = lesson.course_version_id
  AND version.status = 'draft';

-- name: SetCourseVersionClonedCoverFile :execrows
UPDATE course_versions
SET cover_file_id = sqlc.narg(cover_file_id)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(course_version_id)
  AND status = 'draft';
