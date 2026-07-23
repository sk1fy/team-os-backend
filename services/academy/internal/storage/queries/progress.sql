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

-- Legacy progress is a read-only compatibility projection after the Academy
-- cutover. All mutations go through course_enrollments and
-- enrollment_lesson_progress; write queries deliberately do not exist here.
