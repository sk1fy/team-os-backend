-- name: GetProgress :many
SELECT company_id, user_id, course_id, status, completed_lesson_ids, started_at, completed_at
FROM progress
WHERE company_id = $1
ORDER BY course_id, user_id;

-- name: GetCourseProgress :many
SELECT company_id, user_id, course_id, status, completed_lesson_ids, started_at, completed_at
FROM progress
WHERE company_id = $1 AND course_id = $2
ORDER BY user_id;

-- name: GetUserProgressRows :many
SELECT company_id, user_id, course_id, status, completed_lesson_ids, started_at, completed_at
FROM progress
WHERE company_id = $1 AND user_id = $2
ORDER BY course_id;

-- name: GetUserCourseProgressForUpdate :one
SELECT company_id, user_id, course_id, status, completed_lesson_ids, started_at, completed_at
FROM progress
WHERE company_id = $1 AND user_id = $2 AND course_id = $3
FOR UPDATE;

-- name: InsertProgress :one
INSERT INTO progress (company_id, user_id, course_id, status, completed_lesson_ids, started_at, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING company_id, user_id, course_id, status, completed_lesson_ids, started_at, completed_at;

-- name: UpdateProgressRow :one
UPDATE progress
SET status = $4, completed_lesson_ids = $5, started_at = $6, completed_at = $7
WHERE company_id = $1 AND user_id = $2 AND course_id = $3
RETURNING company_id, user_id, course_id, status, completed_lesson_ids, started_at, completed_at;

-- name: RemoveLessonsFromProgress :exec
UPDATE progress
SET completed_lesson_ids = (
    SELECT coalesce(array_agg(kept ORDER BY ordinality), '{}')::uuid[]
    FROM unnest(completed_lesson_ids) WITH ORDINALITY AS t(kept, ordinality)
    WHERE kept <> ALL(sqlc.arg(lesson_ids)::uuid[])
)
WHERE company_id = $1 AND course_id = $2
  AND completed_lesson_ids && sqlc.arg(lesson_ids)::uuid[];

-- name: MarkProgressOverdue :execrows
UPDATE progress
SET status = 'overdue'
WHERE company_id = $1 AND course_id = $2
  AND user_id = ANY(sqlc.arg(user_ids)::uuid[])
  AND status IN ('not_started', 'in_progress');

-- name: InsertOverdueProgress :exec
INSERT INTO progress (company_id, user_id, course_id, status, completed_lesson_ids)
SELECT $1, candidate, $2, 'overdue', '{}'::uuid[]
FROM unnest(sqlc.arg(user_ids)::uuid[]) AS candidate
ON CONFLICT (user_id, course_id) DO NOTHING;
