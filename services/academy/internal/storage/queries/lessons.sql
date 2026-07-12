-- name: GetLessons :many
SELECT id, company_id, course_id, section_id, title, "order", content,
    source_article_id, source_article_title, source_mode, quiz_id
FROM lessons
WHERE company_id = $1
ORDER BY "order", id;

-- name: GetCourseLessons :many
SELECT id, company_id, course_id, section_id, title, "order", content,
    source_article_id, source_article_title, source_mode, quiz_id
FROM lessons
WHERE company_id = $1 AND course_id = $2
ORDER BY "order", id;

-- name: GetLesson :one
SELECT id, company_id, course_id, section_id, title, "order", content,
    source_article_id, source_article_title, source_mode, quiz_id
FROM lessons
WHERE company_id = $1 AND id = $2;

-- name: GetSectionLessonsForUpdate :many
SELECT id, "order"
FROM lessons
WHERE company_id = $1 AND section_id = $2
ORDER BY "order", id
FOR UPDATE;

-- name: CountSectionLessons :one
SELECT count(*)
FROM lessons
WHERE company_id = $1 AND section_id = $2;

-- name: GetCourseLessonIds :many
SELECT id
FROM lessons
WHERE company_id = $1 AND course_id = $2;

-- name: GetSectionLessonIds :many
SELECT id
FROM lessons
WHERE company_id = $1 AND section_id = $2;

-- name: CreateLesson :one
INSERT INTO lessons (
    id, company_id, course_id, section_id, title, "order", content,
    source_article_id, source_article_title, source_mode
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, company_id, course_id, section_id, title, "order", content,
    source_article_id, source_article_title, source_mode, quiz_id;

-- name: UpdateLesson :one
UPDATE lessons
SET title = coalesce(sqlc.narg(title), title),
    content = coalesce(sqlc.narg(content), content),
    source_article_id = CASE WHEN sqlc.arg(set_source_article)::boolean THEN sqlc.narg(source_article_id) ELSE source_article_id END,
    source_article_title = CASE WHEN sqlc.arg(set_source_article_title)::boolean THEN sqlc.narg(source_article_title) ELSE source_article_title END,
    source_mode = CASE WHEN sqlc.arg(set_source_mode)::boolean THEN sqlc.narg(source_mode) ELSE source_mode END
WHERE company_id = $1 AND id = $2
RETURNING id, company_id, course_id, section_id, title, "order", content,
    source_article_id, source_article_title, source_mode, quiz_id;

-- name: SetLessonQuiz :exec
UPDATE lessons
SET quiz_id = $3
WHERE company_id = $1 AND id = $2;

-- name: MoveLessonRow :one
UPDATE lessons
SET section_id = $3, "order" = $4
WHERE company_id = $1 AND id = $2
RETURNING id, company_id, course_id, section_id, title, "order", content,
    source_article_id, source_article_title, source_mode, quiz_id;

-- name: SetLessonOrder :exec
UPDATE lessons
SET "order" = $3
WHERE company_id = $1 AND id = $2;

-- name: DeleteLesson :execrows
DELETE FROM lessons
WHERE company_id = $1 AND id = $2;

-- name: ReplicateLinkedArticle :execrows
UPDATE lessons
SET content = sqlc.arg(content),
    title = CASE WHEN title = source_article_title THEN sqlc.arg(new_title)::text ELSE title END,
    source_article_title = sqlc.arg(new_title)::text
WHERE source_article_id = sqlc.arg(article_id) AND source_mode = 'link';

-- name: DetachLinkedArticle :execrows
UPDATE lessons
SET source_mode = 'copy'
WHERE source_article_id = sqlc.arg(article_id) AND source_mode = 'link';
