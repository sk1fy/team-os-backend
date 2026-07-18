-- name: GetCourseSections :many
SELECT id, company_id, course_id, title, "order"
FROM course_sections
WHERE company_id = $1 AND course_id = $2
ORDER BY "order", id;

-- name: GetPublicCourseSections :many
SELECT id, company_id, course_id, title, "order"
FROM course_sections
WHERE course_id = $1
ORDER BY "order", id;

-- name: GetCourseSection :one
SELECT id, company_id, course_id, title, "order"
FROM course_sections
WHERE company_id = $1 AND id = $2;

-- name: CountCourseSections :one
SELECT count(*)
FROM course_sections
WHERE company_id = $1 AND course_id = $2;

-- name: CreateCourseSection :one
INSERT INTO course_sections (id, company_id, course_id, title, "order")
VALUES ($1, $2, $3, $4, $5)
RETURNING id, company_id, course_id, title, "order";

-- name: UpdateCourseSection :one
UPDATE course_sections
SET title = $3
WHERE company_id = $1 AND id = $2
RETURNING id, company_id, course_id, title, "order";

-- name: DeleteCourseSection :execrows
DELETE FROM course_sections
WHERE company_id = $1 AND id = $2;
