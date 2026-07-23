-- name: ListCourseTemplateVersionSections :many
SELECT id, company_id, template_version_id, stable_key, title, "order"
FROM course_template_version_sections
WHERE company_id = sqlc.arg(company_id)
  AND template_version_id = sqlc.arg(template_version_id)
ORDER BY "order", id;

-- name: CreateCourseTemplateVersionSection :one
INSERT INTO course_template_version_sections (
    id, company_id, template_version_id, stable_key, title, "order"
) SELECT sqlc.arg(id), version.company_id, version.id,
    sqlc.arg(stable_key), sqlc.arg(title), sqlc.arg(order_value)
FROM course_template_versions AS version
JOIN course_templates AS template
  ON template.company_id = version.company_id
 AND template.id = version.template_id
WHERE version.company_id = sqlc.arg(company_id)
  AND version.id = sqlc.arg(template_version_id)
  AND version.status = 'draft'
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active'
RETURNING id, company_id, template_version_id, stable_key, title, "order";

-- name: UpdateCourseTemplateVersionSection :one
UPDATE course_template_version_sections AS section
SET title = COALESCE(sqlc.narg(title)::text, section.title),
    "order" = COALESCE(sqlc.narg(order_value)::integer, section."order")
FROM course_template_versions AS version
JOIN course_templates AS template
  ON template.company_id = version.company_id
 AND template.id = version.template_id
WHERE section.company_id = sqlc.arg(company_id)
  AND section.id = sqlc.arg(id)
  AND version.company_id = section.company_id
  AND version.id = section.template_version_id
  AND version.status = 'draft'
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active'
RETURNING section.id, section.company_id, section.template_version_id,
    section.stable_key, section.title, section."order";

-- name: DeleteCourseTemplateVersionSection :execrows
DELETE FROM course_template_version_sections AS section
USING course_template_versions AS version, course_templates AS template
WHERE section.company_id = sqlc.arg(company_id)
  AND section.id = sqlc.arg(id)
  AND version.company_id = section.company_id
  AND version.id = section.template_version_id
  AND version.status = 'draft'
  AND template.company_id = version.company_id
  AND template.id = version.template_id
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active';

-- name: ListCourseTemplateVersionLessons :many
SELECT id, company_id, template_version_id, section_version_id, stable_key,
    title, "order", content, source_type, kb_snapshot_id,
    estimated_minutes, file_ids, quiz_version_id
FROM course_template_version_lessons
WHERE company_id = sqlc.arg(company_id)
  AND template_version_id = sqlc.arg(template_version_id)
ORDER BY section_version_id, "order", id;

-- name: GetCourseTemplateVersionLesson :one
SELECT id, company_id, template_version_id, section_version_id, stable_key,
    title, "order", content, source_type, kb_snapshot_id,
    estimated_minutes, file_ids, quiz_version_id
FROM course_template_version_lessons
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id);

-- name: CreateCourseTemplateVersionLesson :one
INSERT INTO course_template_version_lessons (
    id, company_id, template_version_id, section_version_id, stable_key,
    title, "order", content, source_type, kb_snapshot_id, estimated_minutes,
    file_ids
) SELECT sqlc.arg(id), version.company_id, version.id,
    section.id, sqlc.arg(stable_key), sqlc.arg(title), sqlc.arg(order_value),
    sqlc.arg(content), sqlc.arg(source_type), sqlc.narg(kb_snapshot_id),
    sqlc.narg(estimated_minutes),
    COALESCE(sqlc.narg(file_ids)::uuid[], '{}'::uuid[])
FROM course_template_versions AS version
JOIN course_templates AS template
  ON template.company_id = version.company_id
 AND template.id = version.template_id
JOIN course_template_version_sections AS section
  ON section.company_id = version.company_id
 AND section.template_version_id = version.id
 AND section.id = sqlc.arg(section_version_id)
WHERE version.company_id = sqlc.arg(company_id)
  AND version.id = sqlc.arg(template_version_id)
  AND version.status = 'draft'
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active'
RETURNING id, company_id, template_version_id, section_version_id, stable_key,
    title, "order", content, source_type, kb_snapshot_id,
    estimated_minutes, file_ids, quiz_version_id;

-- name: UpdateCourseTemplateVersionLesson :one
UPDATE course_template_version_lessons AS lesson
SET section_version_id = COALESCE(
        sqlc.narg(section_version_id)::uuid, lesson.section_version_id),
    title = COALESCE(sqlc.narg(title)::text, lesson.title),
    "order" = COALESCE(sqlc.narg(order_value)::integer, lesson."order"),
    content = COALESCE(sqlc.narg(content)::jsonb, lesson.content),
    source_type = COALESCE(sqlc.narg(source_type)::text, lesson.source_type),
    kb_snapshot_id = CASE
        WHEN sqlc.narg(clear_kb_snapshot)::boolean THEN NULL
        ELSE COALESCE(sqlc.narg(kb_snapshot_id)::uuid, lesson.kb_snapshot_id)
    END,
    estimated_minutes = CASE
        WHEN sqlc.narg(clear_estimated_minutes)::boolean THEN NULL
        ELSE COALESCE(
            sqlc.narg(estimated_minutes)::integer, lesson.estimated_minutes)
    END,
    file_ids = COALESCE(sqlc.narg(file_ids)::uuid[], lesson.file_ids)
FROM course_template_versions AS version
JOIN course_templates AS template
  ON template.company_id = version.company_id
 AND template.id = version.template_id
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.id = sqlc.arg(id)
  AND version.company_id = lesson.company_id
  AND version.id = lesson.template_version_id
  AND version.status = 'draft'
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active'
RETURNING lesson.id, lesson.company_id, lesson.template_version_id,
    lesson.section_version_id, lesson.stable_key, lesson.title, lesson."order",
    lesson.content, lesson.source_type, lesson.kb_snapshot_id,
    lesson.estimated_minutes, lesson.file_ids, lesson.quiz_version_id;

-- name: DeleteCourseTemplateVersionLesson :execrows
DELETE FROM course_template_version_lessons AS lesson
USING course_template_versions AS version, course_templates AS template
WHERE lesson.company_id = sqlc.arg(company_id)
  AND lesson.id = sqlc.arg(id)
  AND version.company_id = lesson.company_id
  AND version.id = lesson.template_version_id
  AND version.status = 'draft'
  AND template.company_id = version.company_id
  AND template.id = version.template_id
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active';

-- name: ListCourseTemplateVersionQuizzes :many
SELECT id, company_id, template_version_id, lesson_version_id,
    questions, passing_score, max_attempts
FROM course_template_version_quizzes
WHERE company_id = sqlc.arg(company_id)
  AND template_version_id = sqlc.arg(template_version_id)
ORDER BY id;

-- name: UpsertCourseTemplateVersionQuiz :one
WITH target AS (
    SELECT lesson.company_id, lesson.template_version_id, lesson.id AS lesson_id
    FROM course_template_version_lessons AS lesson
    JOIN course_template_versions AS version
      ON version.company_id = lesson.company_id
     AND version.id = lesson.template_version_id
    JOIN course_templates AS template
      ON template.company_id = version.company_id
     AND template.id = version.template_id
    WHERE lesson.company_id = sqlc.arg(company_id)
      AND lesson.id = sqlc.arg(lesson_version_id)
      AND version.status = 'draft'
      AND template.template_type = 'company'
      AND template.lifecycle_status = 'active'
), saved AS (
    INSERT INTO course_template_version_quizzes (
        id, company_id, template_version_id, lesson_version_id,
        questions, passing_score, max_attempts
    ) SELECT sqlc.arg(id), target.company_id, target.template_version_id,
        target.lesson_id, sqlc.arg(questions), sqlc.arg(passing_score),
        sqlc.narg(max_attempts)
    FROM target
    ON CONFLICT (lesson_version_id) DO UPDATE
    SET questions = EXCLUDED.questions,
        passing_score = EXCLUDED.passing_score,
        max_attempts = EXCLUDED.max_attempts
    RETURNING id, company_id, template_version_id, lesson_version_id,
        questions, passing_score, max_attempts
), linked AS (
    UPDATE course_template_version_lessons AS lesson
    SET quiz_version_id = saved.id
    FROM saved WHERE lesson.id = saved.lesson_version_id
)
SELECT id, company_id, template_version_id, lesson_version_id,
    questions, passing_score, max_attempts
FROM saved;

-- name: DeleteCourseTemplateVersionQuiz :execrows
WITH target AS (
    SELECT quiz.id, quiz.lesson_version_id
    FROM course_template_version_quizzes AS quiz
    JOIN course_template_versions AS version
      ON version.company_id = quiz.company_id
     AND version.id = quiz.template_version_id
    JOIN course_templates AS template
      ON template.company_id = version.company_id
     AND template.id = version.template_id
    WHERE quiz.company_id = sqlc.arg(company_id)
      AND quiz.id = sqlc.arg(id)
      AND version.status = 'draft'
      AND template.template_type = 'company'
      AND template.lifecycle_status = 'active'
), unlinked AS (
    UPDATE course_template_version_lessons AS lesson
    SET quiz_version_id = NULL
    FROM target WHERE lesson.id = target.lesson_version_id
)
DELETE FROM course_template_version_quizzes AS quiz
USING target WHERE quiz.id = target.id;

-- name: CloneCourseTemplateVersionSections :execrows
INSERT INTO course_template_version_sections (
    id, company_id, template_version_id, stable_key, title, "order"
)
SELECT gen_random_uuid(), target.company_id, target.id,
    source_section.stable_key, source_section.title, source_section."order"
FROM course_template_version_sections AS source_section
JOIN course_template_versions AS source
  ON source.company_id = source_section.company_id
 AND source.id = source_section.template_version_id
JOIN course_template_versions AS target
  ON target.company_id = source.company_id
 AND target.id = sqlc.arg(target_version_id)
WHERE source.company_id = sqlc.arg(company_id)
  AND source.id = sqlc.arg(source_version_id)
  AND source.status IN ('published', 'retired')
  AND target.template_id = source.template_id
  AND target.status = 'draft'
ORDER BY source_section."order", source_section.id;

-- name: CloneCourseTemplateVersionLessons :execrows
INSERT INTO course_template_version_lessons (
    id, company_id, template_version_id, section_version_id, stable_key,
    title, "order", content, source_type, kb_snapshot_id, estimated_minutes,
    file_ids
)
SELECT gen_random_uuid(), target.company_id, target.id, target_section.id,
    source_lesson.stable_key, source_lesson.title, source_lesson."order",
    source_lesson.content, source_lesson.source_type,
    source_lesson.kb_snapshot_id, source_lesson.estimated_minutes,
    source_lesson.file_ids
FROM course_template_version_lessons AS source_lesson
JOIN course_template_version_sections AS source_section
  ON source_section.id = source_lesson.section_version_id
JOIN course_template_versions AS source
  ON source.company_id = source_lesson.company_id
 AND source.id = source_lesson.template_version_id
JOIN course_template_versions AS target
  ON target.company_id = source.company_id
 AND target.id = sqlc.arg(target_version_id)
JOIN course_template_version_sections AS target_section
  ON target_section.template_version_id = target.id
 AND target_section.stable_key = source_section.stable_key
WHERE source.company_id = sqlc.arg(company_id)
  AND source.id = sqlc.arg(source_version_id)
  AND source.status IN ('published', 'retired')
  AND target.template_id = source.template_id
  AND target.status = 'draft'
ORDER BY target_section."order", source_lesson."order", source_lesson.id;

-- name: CloneCourseTemplateVersionQuizzes :execrows
WITH inserted AS (
    INSERT INTO course_template_version_quizzes (
        id, company_id, template_version_id, lesson_version_id,
        questions, passing_score, max_attempts
    )
    SELECT gen_random_uuid(), target.company_id, target.id, target_lesson.id,
        source_quiz.questions, source_quiz.passing_score,
        source_quiz.max_attempts
    FROM course_template_version_quizzes AS source_quiz
    JOIN course_template_version_lessons AS source_lesson
      ON source_lesson.id = source_quiz.lesson_version_id
    JOIN course_template_versions AS source
      ON source.company_id = source_quiz.company_id
     AND source.id = source_quiz.template_version_id
    JOIN course_template_versions AS target
      ON target.company_id = source.company_id
     AND target.id = sqlc.arg(target_version_id)
    JOIN course_template_version_lessons AS target_lesson
      ON target_lesson.template_version_id = target.id
     AND target_lesson.stable_key = source_lesson.stable_key
    WHERE source.company_id = sqlc.arg(company_id)
      AND source.id = sqlc.arg(source_version_id)
      AND source.status IN ('published', 'retired')
      AND target.template_id = source.template_id
      AND target.status = 'draft'
    RETURNING id, lesson_version_id
)
UPDATE course_template_version_lessons AS lesson
SET quiz_version_id = inserted.id
FROM inserted WHERE lesson.id = inserted.lesson_version_id;

-- name: InstantiateCourseTemplateSections :execrows
INSERT INTO course_version_sections (
    id, company_id, course_version_id, stable_key, title, "order"
)
SELECT gen_random_uuid(), target.company_id, target.id,
    source_section.stable_key, source_section.title, source_section."order"
FROM course_template_version_sections AS source_section
JOIN course_template_versions AS source
  ON source.company_id = source_section.company_id
 AND source.id = source_section.template_version_id
JOIN course_templates AS template
  ON template.company_id = source.company_id
 AND template.id = source.template_id
JOIN course_versions AS target
  ON target.company_id = source.company_id
 AND target.id = sqlc.arg(target_course_version_id)
WHERE source.company_id = sqlc.arg(company_id)
  AND source.id = sqlc.arg(source_template_version_id)
  AND source.status = 'published'
  AND template.lifecycle_status = 'active'
  AND target.status = 'draft'
ORDER BY source_section."order", source_section.id;

-- name: InstantiateCourseTemplateLessons :execrows
INSERT INTO course_version_lessons (
    id, company_id, course_version_id, section_version_id, stable_key,
    title, "order", content, source_type, source_template_id,
    source_template_version_id, estimated_minutes, file_ids
)
SELECT gen_random_uuid(), target.company_id, target.id, target_section.id,
    source_lesson.stable_key, source_lesson.title, source_lesson."order",
    source_lesson.content, 'template_snapshot', template.id, source.id,
    source_lesson.estimated_minutes, source_lesson.file_ids
FROM course_template_version_lessons AS source_lesson
JOIN course_template_version_sections AS source_section
  ON source_section.id = source_lesson.section_version_id
JOIN course_template_versions AS source
  ON source.company_id = source_lesson.company_id
 AND source.id = source_lesson.template_version_id
JOIN course_templates AS template
  ON template.company_id = source.company_id
 AND template.id = source.template_id
JOIN course_versions AS target
  ON target.company_id = source.company_id
 AND target.id = sqlc.arg(target_course_version_id)
JOIN course_version_sections AS target_section
  ON target_section.course_version_id = target.id
 AND target_section.stable_key = source_section.stable_key
WHERE source.company_id = sqlc.arg(company_id)
  AND source.id = sqlc.arg(source_template_version_id)
  AND source.status = 'published'
  AND template.lifecycle_status = 'active'
  AND target.status = 'draft'
ORDER BY target_section."order", source_lesson."order", source_lesson.id;

-- name: InstantiateCourseTemplateQuizzes :execrows
WITH inserted AS (
    INSERT INTO course_version_quizzes (
        id, company_id, course_version_id, lesson_version_id,
        questions, passing_score, max_attempts
    )
    SELECT gen_random_uuid(), target.company_id, target.id, target_lesson.id,
        source_quiz.questions, source_quiz.passing_score,
        source_quiz.max_attempts
    FROM course_template_version_quizzes AS source_quiz
    JOIN course_template_version_lessons AS source_lesson
      ON source_lesson.id = source_quiz.lesson_version_id
    JOIN course_template_versions AS source
      ON source.company_id = source_quiz.company_id
     AND source.id = source_quiz.template_version_id
    JOIN course_templates AS template
      ON template.company_id = source.company_id
     AND template.id = source.template_id
    JOIN course_versions AS target
      ON target.company_id = source.company_id
     AND target.id = sqlc.arg(target_course_version_id)
    JOIN course_version_lessons AS target_lesson
      ON target_lesson.course_version_id = target.id
     AND target_lesson.stable_key = source_lesson.stable_key
    WHERE source.company_id = sqlc.arg(company_id)
      AND source.id = sqlc.arg(source_template_version_id)
      AND source.status = 'published'
      AND template.lifecycle_status = 'active'
      AND target.status = 'draft'
    RETURNING id, lesson_version_id
)
UPDATE course_version_lessons AS lesson
SET quiz_version_id = inserted.id
FROM inserted WHERE lesson.id = inserted.lesson_version_id;

-- name: ListCourseTemplateVersionFileIDs :many
SELECT file_id::uuid
FROM (
    SELECT version.cover_file_id AS file_id
    FROM course_template_versions AS version
    WHERE version.company_id = sqlc.arg(company_id)
      AND version.id = sqlc.arg(template_version_id)
      AND version.cover_file_id IS NOT NULL
    UNION
    SELECT unnest(lesson.file_ids) AS file_id
    FROM course_template_version_lessons AS lesson
    WHERE lesson.company_id = sqlc.arg(company_id)
      AND lesson.template_version_id = sqlc.arg(template_version_id)
    UNION
    SELECT unnest(snapshot.source_file_ids) AS file_id
    FROM course_template_version_lessons AS lesson
    JOIN kb_article_snapshots AS snapshot
      ON snapshot.company_id = lesson.company_id
     AND snapshot.id = lesson.kb_snapshot_id
    WHERE lesson.company_id = sqlc.arg(company_id)
      AND lesson.template_version_id = sqlc.arg(template_version_id)
) AS files
ORDER BY file_id;
