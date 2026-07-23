-- name: GetCourseVersions :many
SELECT id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash
FROM course_versions
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
ORDER BY number DESC, id;

-- name: GetCourseVersion :one
SELECT id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash
FROM course_versions
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id);

-- name: GetCourseVersionForUpdate :one
SELECT id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash
FROM course_versions
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
FOR UPDATE;

-- name: GetCourseVersionByNumber :one
SELECT id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash
FROM course_versions
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND number = sqlc.arg(number);

-- name: GetCurrentDraftCourseVersion :one
SELECT version.id, version.company_id, version.course_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.cover_url, version.sequential,
    version.default_internal_deadline_days, version.created_by_id,
    version.created_at, version.published_by_id, version.published_at,
    version.content_hash
FROM courses AS course
JOIN course_versions AS version ON version.id = course.current_draft_version_id
WHERE course.company_id = sqlc.arg(company_id)
  AND course.id = sqlc.arg(course_id);

-- name: LockCourseAndCurrentDraftVersion :one
SELECT version.id, version.company_id, version.course_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.cover_url, version.sequential,
    version.default_internal_deadline_days, version.created_by_id,
    version.created_at, version.published_by_id, version.published_at,
    version.content_hash
FROM courses AS course
JOIN course_versions AS version ON version.id = course.current_draft_version_id
WHERE course.company_id = sqlc.arg(company_id)
  AND course.id = sqlc.arg(course_id)
FOR UPDATE OF course, version;

-- name: GetLatestPublishedCourseVersion :one
SELECT version.id, version.company_id, version.course_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.cover_url, version.sequential,
    version.default_internal_deadline_days, version.created_by_id,
    version.created_at, version.published_by_id, version.published_at,
    version.content_hash
FROM courses AS course
JOIN course_versions AS version ON version.id = course.latest_published_version_id
WHERE course.company_id = sqlc.arg(company_id)
  AND course.id = sqlc.arg(course_id);

-- name: GetNextCourseVersionNumber :one
SELECT COALESCE(max(number), 0)::integer + 1
FROM course_versions
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id);

-- name: CreateCourseVersion :one
INSERT INTO course_versions (
    id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(course_id), sqlc.arg(number),
    'draft', sqlc.arg(title), sqlc.narg(description), sqlc.narg(cover_file_id),
    sqlc.narg(cover_url), sqlc.arg(sequential),
    sqlc.narg(default_internal_deadline_days), sqlc.arg(created_by_id),
    sqlc.arg(created_at)
)
RETURNING id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash;

-- name: CreateNextDraftCourseVersionFromPublished :one
INSERT INTO course_versions (
    id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at
)
SELECT sqlc.arg(id), source.company_id, source.course_id,
       source.number + 1, 'draft', source.title, source.description,
       source.cover_file_id, source.cover_url, source.sequential,
       source.default_internal_deadline_days, sqlc.arg(created_by_id),
       sqlc.arg(created_at)
FROM course_versions AS source
WHERE source.company_id = sqlc.arg(company_id)
  AND source.id = sqlc.arg(source_version_id)
  AND source.status IN ('published', 'retired')
RETURNING id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash;

-- name: UpdateDraftCourseVersion :one
UPDATE course_versions
SET title = sqlc.arg(title),
    description = sqlc.narg(description),
    cover_file_id = sqlc.narg(cover_file_id),
    cover_url = sqlc.narg(cover_url),
    sequential = sqlc.arg(sequential),
    default_internal_deadline_days = sqlc.narg(default_internal_deadline_days),
    content_hash = NULL
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status = 'draft'
RETURNING id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash;

-- name: PublishCourseVersion :one
UPDATE course_versions
SET status = 'published',
    published_by_id = sqlc.arg(published_by_id),
    published_at = sqlc.arg(published_at),
    content_hash = sqlc.arg(content_hash)
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND id = sqlc.arg(id)
  AND status = 'draft'
RETURNING id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash;

-- name: RetireCourseVersion :one
UPDATE course_versions
SET status = 'retired'
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status = 'published'
RETURNING id, company_id, course_id, number, status, title, description,
    cover_file_id, cover_url, sequential, default_internal_deadline_days,
    created_by_id, created_at, published_by_id, published_at, content_hash;

-- name: DeleteDraftCourseVersion :execrows
DELETE FROM course_versions
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status = 'draft';

-- name: SetCourseCurrentDraftVersion :execrows
UPDATE courses AS course
SET current_draft_version_id = version.id,
    updated_at = sqlc.arg(updated_at)
FROM course_versions AS version
WHERE course.company_id = sqlc.arg(company_id)
  AND course.id = sqlc.arg(course_id)
  AND version.company_id = course.company_id
  AND version.course_id = course.id
  AND version.id = sqlc.arg(version_id)
  AND version.status = 'draft';

-- name: ClearCourseCurrentDraftVersion :execrows
UPDATE courses
SET current_draft_version_id = NULL,
    updated_at = sqlc.arg(updated_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(course_id)
  AND current_draft_version_id = sqlc.arg(version_id);

-- name: SetCoursePublishedVersionPointers :execrows
UPDATE courses AS course
SET current_draft_version_id = NULL,
    latest_published_version_id = version.id,
    status = 'published',
    title = version.title,
    description = version.description,
    cover_url = version.cover_url,
    sequential = version.sequential,
    deadline_days = version.default_internal_deadline_days,
    updated_at = sqlc.arg(updated_at)
FROM course_versions AS version
WHERE course.company_id = sqlc.arg(company_id)
  AND course.id = sqlc.arg(course_id)
  AND course.current_draft_version_id = version.id
  AND version.company_id = course.company_id
  AND version.course_id = course.id
  AND version.id = sqlc.arg(version_id)
  AND version.status = 'published';

-- name: GetCourseVersionPublishIdempotency :one
SELECT id, company_id, course_id, idempotency_key, version_id, created_at
FROM course_version_publish_idempotency
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: GetCourseVersionPublishIdempotencyForUpdate :one
SELECT id, company_id, course_id, idempotency_key, version_id, created_at
FROM course_version_publish_idempotency
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND idempotency_key = sqlc.arg(idempotency_key)
FOR UPDATE;

-- name: CreateCourseVersionPublishIdempotency :one
INSERT INTO course_version_publish_idempotency (
    id, company_id, course_id, idempotency_key, version_id, created_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(course_id),
    sqlc.arg(idempotency_key), sqlc.arg(version_id), sqlc.arg(created_at)
)
ON CONFLICT (company_id, course_id, idempotency_key) DO NOTHING
RETURNING id, company_id, course_id, idempotency_key, version_id, created_at;

-- name: GetCourseVersionSections :many
SELECT id, company_id, course_version_id, stable_key, title, "order"
FROM course_version_sections
WHERE company_id = sqlc.arg(company_id)
  AND course_version_id = sqlc.arg(course_version_id)
ORDER BY "order", id;

-- name: GetCourseVersionSection :one
SELECT id, company_id, course_version_id, stable_key, title, "order"
FROM course_version_sections
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id);

-- name: GetCourseVersionBySection :one
SELECT version.id, version.company_id, version.course_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.cover_url, version.sequential,
    version.default_internal_deadline_days, version.created_by_id,
    version.created_at, version.published_by_id, version.published_at,
    version.content_hash
FROM course_version_sections AS section
JOIN course_versions AS version ON version.id = section.course_version_id
WHERE section.company_id = sqlc.arg(company_id)
  AND section.id = sqlc.arg(section_id);

-- name: CreateCourseVersionSection :one
INSERT INTO course_version_sections (
    id, company_id, course_version_id, stable_key, title, "order"
)
SELECT sqlc.arg(id), version.company_id, version.id, sqlc.arg(stable_key),
       sqlc.arg(title), sqlc.arg(order_value)
FROM course_versions AS version
WHERE version.company_id = sqlc.arg(company_id)
  AND version.id = sqlc.arg(course_version_id)
  AND version.status = 'draft'
RETURNING id, company_id, course_version_id, stable_key, title, "order";

-- name: UpdateCourseVersionSection :one
UPDATE course_version_sections AS section
SET title = COALESCE(sqlc.narg(title)::text, section.title),
    "order" = COALESCE(sqlc.narg(order_value)::integer, section."order")
FROM course_versions AS version
WHERE section.company_id = sqlc.arg(company_id)
  AND section.id = sqlc.arg(id)
  AND version.id = section.course_version_id
  AND version.status = 'draft'
RETURNING section.id, section.company_id, section.course_version_id,
    section.stable_key, section.title, section."order";

-- name: DeleteCourseVersionSection :execrows
DELETE FROM course_version_sections AS section
USING course_versions AS version
WHERE section.company_id = sqlc.arg(company_id)
  AND section.id = sqlc.arg(id)
  AND version.id = section.course_version_id
  AND version.status = 'draft';

-- name: CloneCourseVersionSections :execrows
INSERT INTO course_version_sections (
    id, company_id, course_version_id, stable_key, title, "order"
)
SELECT gen_random_uuid(), target.company_id, target.id, source.stable_key,
       source.title, source."order"
FROM course_version_sections AS source
JOIN course_versions AS target
  ON target.company_id = source.company_id
 AND target.id = sqlc.arg(target_version_id)
 AND target.status = 'draft'
WHERE source.company_id = sqlc.arg(company_id)
  AND source.course_version_id = sqlc.arg(source_version_id)
ORDER BY source."order", source.id;

-- name: GetCourseVersionLessons :many
SELECT id, company_id, course_version_id, section_version_id, stable_key,
    title, "order", content, source_type, source_article_id,
    source_article_version, source_template_id, source_template_version_id,
    estimated_minutes, quiz_version_id, kb_snapshot_id, file_ids
FROM course_version_lessons
WHERE company_id = sqlc.arg(company_id)
  AND course_version_id = sqlc.arg(course_version_id)
ORDER BY section_version_id, "order", id;

-- name: GetCourseVersionLesson :one
SELECT id, company_id, course_version_id, section_version_id, stable_key,
    title, "order", content, source_type, source_article_id,
    source_article_version, source_template_id, source_template_version_id,
    estimated_minutes, quiz_version_id, kb_snapshot_id, file_ids
FROM course_version_lessons
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id);

-- name: GetCourseVersionByLesson :one
SELECT version.id, version.company_id, version.course_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.cover_url, version.sequential,
    version.default_internal_deadline_days, version.created_by_id,
    version.created_at, version.published_by_id, version.published_at,
    version.content_hash
FROM course_version_lessons AS lesson
JOIN course_versions AS version ON version.id = lesson.course_version_id
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.id = sqlc.arg(lesson_id);

-- name: CreateCourseVersionLesson :one
INSERT INTO course_version_lessons (
    id, company_id, course_version_id, section_version_id, stable_key,
    title, "order", content, source_type, source_article_id,
    source_article_version, source_template_id, source_template_version_id,
    estimated_minutes, file_ids, kb_snapshot_id
)
SELECT sqlc.arg(id), version.company_id, version.id,
       sqlc.arg(section_version_id), sqlc.arg(stable_key), sqlc.arg(title),
       sqlc.arg(order_value), sqlc.arg(content), sqlc.arg(source_type),
       sqlc.narg(source_article_id), sqlc.narg(source_article_version),
       sqlc.narg(source_template_id), sqlc.narg(source_template_version_id),
       sqlc.narg(estimated_minutes),
       COALESCE(sqlc.narg(file_ids)::uuid[], '{}'::uuid[]),
       sqlc.narg(kb_snapshot_id)
FROM course_versions AS version
JOIN course_version_sections AS section
  ON section.company_id = version.company_id
 AND section.course_version_id = version.id
 AND section.id = sqlc.arg(section_version_id)
WHERE version.company_id = sqlc.arg(company_id)
  AND version.id = sqlc.arg(course_version_id)
  AND version.status = 'draft'
RETURNING id, company_id, course_version_id, section_version_id, stable_key,
    title, "order", content, source_type, source_article_id,
    source_article_version, source_template_id, source_template_version_id,
    estimated_minutes, quiz_version_id, kb_snapshot_id, file_ids;

-- name: UpdateCourseVersionLesson :one
UPDATE course_version_lessons AS lesson
SET title = sqlc.arg(title),
    content = sqlc.arg(content),
    source_type = sqlc.arg(source_type),
    source_article_id = sqlc.narg(source_article_id),
    source_article_version = sqlc.narg(source_article_version),
    source_template_id = sqlc.narg(source_template_id),
    source_template_version_id = sqlc.narg(source_template_version_id),
    estimated_minutes = sqlc.narg(estimated_minutes),
    file_ids = COALESCE(sqlc.narg(file_ids)::uuid[], lesson.file_ids),
    kb_snapshot_id = sqlc.narg(kb_snapshot_id)
FROM course_versions AS version
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.id = sqlc.arg(id)
  AND version.id = lesson.course_version_id
  AND version.status = 'draft'
RETURNING lesson.id, lesson.company_id, lesson.course_version_id,
    lesson.section_version_id, lesson.stable_key, lesson.title, lesson."order",
    lesson.content, lesson.source_type, lesson.source_article_id,
    lesson.source_article_version, lesson.source_template_id,
    lesson.source_template_version_id, lesson.estimated_minutes,
    lesson.quiz_version_id, lesson.kb_snapshot_id, lesson.file_ids;

-- name: ReplicateLinkedArticleInDraftVersions :execrows
UPDATE course_version_lessons AS lesson
SET title = sqlc.arg(new_title),
    content = sqlc.arg(content),
    source_article_version = sqlc.arg(article_version)
FROM course_versions AS version
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.source_type = 'kb_link'
  AND lesson.source_article_id = sqlc.arg(article_id)
  AND version.company_id = lesson.company_id
  AND version.id = lesson.course_version_id
  AND version.status = 'draft';

-- name: MoveCourseVersionLesson :one
UPDATE course_version_lessons AS lesson
SET section_version_id = target_section.id,
    "order" = sqlc.arg(order_value)
FROM course_versions AS version, course_version_sections AS target_section
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.id = sqlc.arg(id)
  AND version.id = lesson.course_version_id
  AND version.status = 'draft'
  AND target_section.company_id = lesson.company_id
  AND target_section.course_version_id = lesson.course_version_id
  AND target_section.id = sqlc.arg(section_version_id)
RETURNING lesson.id, lesson.company_id, lesson.course_version_id,
    lesson.section_version_id, lesson.stable_key, lesson.title, lesson."order",
    lesson.content, lesson.source_type, lesson.source_article_id,
    lesson.source_article_version, lesson.source_template_id,
    lesson.source_template_version_id, lesson.estimated_minutes, lesson.file_ids,
    lesson.quiz_version_id, lesson.kb_snapshot_id;

-- name: SetCourseVersionLessonOrder :execrows
UPDATE course_version_lessons AS lesson
SET "order" = sqlc.arg(order_value)
FROM course_versions AS version
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.id = sqlc.arg(id)
  AND version.id = lesson.course_version_id
  AND version.status = 'draft';

-- name: DeleteCourseVersionLesson :execrows
DELETE FROM course_version_lessons AS lesson
USING course_versions AS version
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.id = sqlc.arg(id)
  AND version.id = lesson.course_version_id
  AND version.status = 'draft';

-- name: CloneCourseVersionLessons :execrows
INSERT INTO course_version_lessons (
    id, company_id, course_version_id, section_version_id, stable_key,
    title, "order", content, source_type, source_article_id,
    source_article_version, source_template_id, source_template_version_id,
    estimated_minutes, file_ids, kb_snapshot_id
)
SELECT gen_random_uuid(), target.company_id, target.id, target_section.id,
       source.stable_key, source.title, source."order", source.content,
       source.source_type, source.source_article_id,
       source.source_article_version, source.source_template_id,
       source.source_template_version_id, source.estimated_minutes,
       source.file_ids, source.kb_snapshot_id
FROM course_version_lessons AS source
JOIN course_version_sections AS source_section
  ON source_section.id = source.section_version_id
JOIN course_versions AS target
  ON target.company_id = source.company_id
 AND target.id = sqlc.arg(target_version_id)
 AND target.status = 'draft'
JOIN course_version_sections AS target_section
  ON target_section.course_version_id = target.id
 AND target_section.stable_key = source_section.stable_key
WHERE source.company_id = sqlc.arg(company_id)
  AND source.course_version_id = sqlc.arg(source_version_id)
ORDER BY target_section."order", source."order", source.id;

-- name: GetCourseVersionQuizzes :many
SELECT id, company_id, course_version_id, lesson_version_id, questions,
    passing_score, max_attempts
FROM course_version_quizzes
WHERE company_id = sqlc.arg(company_id)
  AND course_version_id = sqlc.arg(course_version_id)
ORDER BY id;

-- name: GetCourseVersionQuiz :one
SELECT id, company_id, course_version_id, lesson_version_id, questions,
    passing_score, max_attempts
FROM course_version_quizzes
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id);

-- name: GetCourseVersionByQuiz :one
SELECT version.id, version.company_id, version.course_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.cover_url, version.sequential,
    version.default_internal_deadline_days, version.created_by_id,
    version.created_at, version.published_by_id, version.published_at,
    version.content_hash
FROM course_version_quizzes AS quiz
JOIN course_versions AS version ON version.id = quiz.course_version_id
WHERE quiz.company_id = sqlc.arg(company_id)
  AND quiz.id = sqlc.arg(quiz_id);

-- name: CreateCourseVersionQuiz :one
WITH inserted AS (
    INSERT INTO course_version_quizzes (
        id, company_id, course_version_id, lesson_version_id,
        questions, passing_score, max_attempts
    )
    SELECT sqlc.arg(id), version.company_id, version.id,
           sqlc.arg(lesson_version_id), sqlc.arg(questions),
           sqlc.arg(passing_score), sqlc.narg(max_attempts)
    FROM course_versions AS version
    JOIN course_version_lessons AS lesson
      ON lesson.company_id = version.company_id
     AND lesson.course_version_id = version.id
     AND lesson.id = sqlc.arg(lesson_version_id)
    WHERE version.company_id = sqlc.arg(company_id)
      AND version.id = sqlc.arg(course_version_id)
      AND version.status = 'draft'
    RETURNING id, company_id, course_version_id, lesson_version_id,
        questions, passing_score, max_attempts
), linked AS (
    UPDATE course_version_lessons AS lesson
    SET quiz_version_id = inserted.id
    FROM inserted
    WHERE lesson.id = inserted.lesson_version_id
    RETURNING inserted.*
)
SELECT id, company_id, course_version_id, lesson_version_id, questions,
    passing_score, max_attempts
FROM linked;

-- name: UpdateCourseVersionQuiz :one
UPDATE course_version_quizzes AS quiz
SET questions = sqlc.arg(questions),
    passing_score = sqlc.arg(passing_score),
    max_attempts = sqlc.narg(max_attempts)
FROM course_versions AS version
WHERE quiz.company_id = sqlc.arg(company_id)
  AND quiz.id = sqlc.arg(id)
  AND version.id = quiz.course_version_id
  AND version.status = 'draft'
RETURNING quiz.id, quiz.company_id, quiz.course_version_id,
    quiz.lesson_version_id, quiz.questions, quiz.passing_score,
    quiz.max_attempts;

-- name: DeleteCourseVersionQuiz :execrows
WITH unlinked AS (
    UPDATE course_version_lessons AS lesson
    SET quiz_version_id = NULL
    FROM course_version_quizzes AS quiz, course_versions AS version
    WHERE quiz.company_id = sqlc.arg(company_id)
      AND quiz.id = sqlc.arg(id)
      AND lesson.id = quiz.lesson_version_id
      AND version.id = quiz.course_version_id
      AND version.status = 'draft'
    RETURNING quiz.id
)
DELETE FROM course_version_quizzes AS quiz
USING unlinked
WHERE quiz.id = unlinked.id;

-- name: CloneCourseVersionQuizzes :execrows
WITH inserted AS (
    INSERT INTO course_version_quizzes (
        id, company_id, course_version_id, lesson_version_id,
        questions, passing_score, max_attempts
    )
    SELECT gen_random_uuid(), target.company_id, target.id, target_lesson.id,
           source.questions, source.passing_score, source.max_attempts
    FROM course_version_quizzes AS source
    JOIN course_version_lessons AS source_lesson
      ON source_lesson.id = source.lesson_version_id
    JOIN course_versions AS target
      ON target.company_id = source.company_id
     AND target.id = sqlc.arg(target_version_id)
     AND target.status = 'draft'
    JOIN course_version_lessons AS target_lesson
      ON target_lesson.course_version_id = target.id
     AND target_lesson.stable_key = source_lesson.stable_key
    WHERE source.company_id = sqlc.arg(company_id)
      AND source.course_version_id = sqlc.arg(source_version_id)
    RETURNING id, lesson_version_id
)
UPDATE course_version_lessons AS lesson
SET quiz_version_id = inserted.id
FROM inserted
WHERE lesson.id = inserted.lesson_version_id;
