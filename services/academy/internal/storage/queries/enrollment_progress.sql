-- name: GetEnrollmentLessonProgressForUpdate :one
SELECT company_id, enrollment_id, lesson_version_id, status,
    first_opened_at, completed_at, active_seconds, last_position
FROM enrollment_lesson_progress
WHERE company_id = sqlc.arg(company_id)
  AND enrollment_id = sqlc.arg(enrollment_id)
  AND lesson_version_id = sqlc.arg(lesson_version_id)
FOR UPDATE;

-- name: ListEnrollmentLessonProgress :many
SELECT company_id, enrollment_id, lesson_version_id, status,
    first_opened_at, completed_at, active_seconds, last_position
FROM enrollment_lesson_progress
WHERE company_id = sqlc.arg(company_id)
  AND enrollment_id = sqlc.arg(enrollment_id)
ORDER BY lesson_version_id;

-- name: UpsertEnrollmentLessonProgress :one
INSERT INTO enrollment_lesson_progress (
    company_id, enrollment_id, lesson_version_id, status, first_opened_at,
    completed_at, active_seconds, last_position
)
VALUES (
    sqlc.arg(company_id), sqlc.arg(enrollment_id),
    sqlc.arg(lesson_version_id), sqlc.arg(status),
    sqlc.narg(first_opened_at), sqlc.narg(completed_at),
    sqlc.arg(active_seconds), sqlc.narg(last_position)
)
ON CONFLICT (enrollment_id, lesson_version_id) DO UPDATE
SET status = EXCLUDED.status,
    first_opened_at = COALESCE(
        enrollment_lesson_progress.first_opened_at, EXCLUDED.first_opened_at
    ),
    completed_at = EXCLUDED.completed_at,
    active_seconds = EXCLUDED.active_seconds,
    last_position = EXCLUDED.last_position
WHERE enrollment_lesson_progress.company_id = EXCLUDED.company_id
RETURNING company_id, enrollment_id, lesson_version_id, status,
    first_opened_at, completed_at, active_seconds, last_position;

-- name: SetEnrollmentLessonProgressStatus :one
UPDATE enrollment_lesson_progress
SET status = sqlc.arg(status),
    first_opened_at = COALESCE(first_opened_at, sqlc.narg(first_opened_at)),
    completed_at = sqlc.narg(completed_at),
    active_seconds = active_seconds + sqlc.arg(active_seconds_delta),
    last_position = COALESCE(sqlc.narg(last_position), last_position)
WHERE company_id = sqlc.arg(company_id)
  AND enrollment_id = sqlc.arg(enrollment_id)
  AND lesson_version_id = sqlc.arg(lesson_version_id)
RETURNING company_id, enrollment_id, lesson_version_id, status,
    first_opened_at, completed_at, active_seconds, last_position;

-- name: CompleteCurrentAndUnlockNextLesson :many
WITH completed AS (
    UPDATE enrollment_lesson_progress AS progress
    SET status = 'completed',
        first_opened_at = COALESCE(first_opened_at, sqlc.arg(completed_at)),
        completed_at = COALESCE(progress.completed_at,
                                sqlc.arg(completed_at))
    WHERE progress.company_id = sqlc.arg(company_id)
      AND progress.enrollment_id = sqlc.arg(enrollment_id)
      AND progress.lesson_version_id = sqlc.arg(completed_lesson_version_id)
    RETURNING progress.company_id, progress.enrollment_id,
        progress.lesson_version_id, progress.status,
        progress.first_opened_at, progress.completed_at,
        progress.active_seconds, progress.last_position
), unlocked AS (
    INSERT INTO enrollment_lesson_progress (
        company_id, enrollment_id, lesson_version_id, status,
        first_opened_at, active_seconds
    )
    SELECT sqlc.arg(company_id), completed.enrollment_id,
           sqlc.narg(next_lesson_version_id)::uuid, 'current',
           sqlc.arg(completed_at), 0
    FROM completed
    WHERE sqlc.narg(next_lesson_version_id)::uuid IS NOT NULL
    ON CONFLICT (enrollment_id, lesson_version_id) DO UPDATE
    SET status = 'current',
        first_opened_at = COALESCE(
            enrollment_lesson_progress.first_opened_at,
            EXCLUDED.first_opened_at
        )
    WHERE enrollment_lesson_progress.company_id = EXCLUDED.company_id
    RETURNING enrollment_lesson_progress.company_id,
        enrollment_lesson_progress.enrollment_id,
        enrollment_lesson_progress.lesson_version_id,
        enrollment_lesson_progress.status,
        enrollment_lesson_progress.first_opened_at,
        enrollment_lesson_progress.completed_at,
        enrollment_lesson_progress.active_seconds,
        enrollment_lesson_progress.last_position
)
SELECT company_id, enrollment_id, lesson_version_id, status,
    first_opened_at, completed_at, active_seconds, last_position
FROM completed
UNION ALL
SELECT company_id, enrollment_id, lesson_version_id, status,
    first_opened_at, completed_at, active_seconds, last_position
FROM unlocked
ORDER BY lesson_version_id;
