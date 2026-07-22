-- Expand the legacy mutable course model with immutable content snapshots.
-- The legacy tables and summary columns intentionally remain in place during
-- dual read/write and are backfilled into version 1 below.
ALTER TABLE courses
    ADD COLUMN current_draft_version_id uuid,
    ADD COLUMN latest_published_version_id uuid;

ALTER TABLE courses
    ADD CONSTRAINT courses_company_id_id_key UNIQUE (company_id, id);

CREATE TABLE course_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL,
    number integer NOT NULL CHECK (number >= 1),
    status text NOT NULL CHECK (status IN ('draft', 'published', 'retired')),
    title text NOT NULL CHECK (btrim(title) <> ''),
    description text,
    cover_file_id uuid,
    cover_url text,
    sequential boolean NOT NULL DEFAULT true,
    default_internal_deadline_days integer
        CHECK (default_internal_deadline_days >= 1),
    created_by_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_by_id uuid,
    published_at timestamptz,
    content_hash text,
    CONSTRAINT course_versions_course_fk
        FOREIGN KEY (company_id, course_id)
        REFERENCES courses (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT course_versions_company_course_id_key
        UNIQUE (company_id, course_id, id),
    CONSTRAINT course_versions_company_id_id_key
        UNIQUE (company_id, id),
    CONSTRAINT course_versions_company_course_number_key
        UNIQUE (company_id, course_id, number),
    CONSTRAINT course_versions_course_number_key
        UNIQUE (course_id, number)
);

CREATE UNIQUE INDEX course_versions_one_draft_per_course_uidx
    ON course_versions (course_id)
    WHERE status = 'draft';

CREATE INDEX course_versions_company_course_status_idx
    ON course_versions (company_id, course_id, status, number DESC);

CREATE TABLE course_version_sections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_version_id uuid NOT NULL,
    stable_key uuid NOT NULL DEFAULT gen_random_uuid(),
    title text NOT NULL CHECK (btrim(title) <> ''),
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0),
    CONSTRAINT course_version_sections_version_fk
        FOREIGN KEY (company_id, course_version_id)
        REFERENCES course_versions (company_id, id) ON DELETE CASCADE,
    CONSTRAINT course_version_sections_version_id_key
        UNIQUE (company_id, course_version_id, id),
    CONSTRAINT course_version_sections_stable_key_key
        UNIQUE (course_version_id, stable_key)
);

CREATE INDEX course_version_sections_version_order_idx
    ON course_version_sections (course_version_id, "order", id);

CREATE TABLE course_version_lessons (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_version_id uuid NOT NULL,
    section_version_id uuid NOT NULL,
    stable_key uuid NOT NULL DEFAULT gen_random_uuid(),
    title text NOT NULL CHECK (btrim(title) <> ''),
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0),
    content jsonb NOT NULL,
    source_type text NOT NULL DEFAULT 'manual'
        CHECK (source_type IN ('manual', 'kb_link', 'kb_snapshot', 'template_snapshot')),
    source_article_id uuid,
    source_article_version integer CHECK (source_article_version >= 1),
    source_template_id uuid,
    source_template_version_id uuid,
    estimated_minutes integer CHECK (estimated_minutes >= 1),
    quiz_version_id uuid,
    CONSTRAINT course_version_lessons_section_fk
        FOREIGN KEY (company_id, course_version_id, section_version_id)
        REFERENCES course_version_sections (company_id, course_version_id, id)
        ON DELETE CASCADE,
    CONSTRAINT course_version_lessons_version_id_key
        UNIQUE (company_id, course_version_id, id),
    CONSTRAINT course_version_lessons_stable_key_key
        UNIQUE (course_version_id, stable_key),
    CONSTRAINT course_version_lessons_source_shape_check CHECK (
        (source_type = 'manual'
            AND source_article_id IS NULL
            AND source_article_version IS NULL
            AND source_template_id IS NULL
            AND source_template_version_id IS NULL)
        OR (source_type IN ('kb_link', 'kb_snapshot')
            AND source_article_id IS NOT NULL
            AND source_template_id IS NULL
            AND source_template_version_id IS NULL)
        OR (source_type = 'template_snapshot'
            AND source_article_id IS NULL
            AND source_article_version IS NULL
            AND source_template_id IS NOT NULL)
    )
);

CREATE INDEX course_version_lessons_version_section_order_idx
    ON course_version_lessons (course_version_id, section_version_id, "order", id);

CREATE INDEX course_version_lessons_source_article_idx
    ON course_version_lessons (company_id, source_article_id)
    WHERE source_article_id IS NOT NULL;

CREATE TABLE course_version_quizzes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_version_id uuid NOT NULL,
    lesson_version_id uuid NOT NULL,
    questions jsonb NOT NULL DEFAULT '[]'::jsonb,
    passing_score integer NOT NULL DEFAULT 0 CHECK (passing_score BETWEEN 0 AND 100),
    max_attempts integer CHECK (max_attempts >= 1),
    CONSTRAINT course_version_quizzes_lesson_fk
        FOREIGN KEY (company_id, course_version_id, lesson_version_id)
        REFERENCES course_version_lessons (company_id, course_version_id, id)
        ON DELETE CASCADE,
    CONSTRAINT course_version_quizzes_version_id_key
        UNIQUE (company_id, course_version_id, id),
    CONSTRAINT course_version_quizzes_lesson_key
        UNIQUE (lesson_version_id)
);

CREATE INDEX course_version_quizzes_version_idx
    ON course_version_quizzes (course_version_id, id);

ALTER TABLE course_version_lessons
    ADD CONSTRAINT course_version_lessons_quiz_fk
        FOREIGN KEY (company_id, course_version_id, quiz_version_id)
        REFERENCES course_version_quizzes (company_id, course_version_id, id)
        ON DELETE SET NULL (quiz_version_id);

CREATE TABLE course_version_publish_idempotency (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL,
    idempotency_key text NOT NULL
        CHECK (btrim(idempotency_key) <> '' AND octet_length(idempotency_key) <= 512),
    version_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT course_version_publish_idempotency_key
        UNIQUE (company_id, course_id, idempotency_key),
    CONSTRAINT course_version_publish_idempotency_version_fk
        FOREIGN KEY (company_id, course_id, version_id)
        REFERENCES course_versions (company_id, course_id, id)
        ON DELETE RESTRICT
);

CREATE INDEX course_version_publish_idempotency_version_idx
    ON course_version_publish_idempotency (company_id, version_id);

-- Stop rather than silently dropping or re-parenting malformed legacy rows.
-- These inconsistencies are not representable in the tenant-safe version model.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM course_sections AS section
        JOIN courses AS course ON course.id = section.course_id
        WHERE section.company_id <> course.company_id
    ) THEN
        RAISE EXCEPTION 'academy version backfill: course section tenant mismatch';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM lessons AS lesson
        JOIN courses AS course ON course.id = lesson.course_id
        JOIN course_sections AS section ON section.id = lesson.section_id
        WHERE lesson.company_id <> course.company_id
           OR section.company_id <> course.company_id
           OR section.course_id <> lesson.course_id
    ) THEN
        RAISE EXCEPTION 'academy version backfill: lesson course, section or tenant mismatch';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM quizzes AS quiz
        JOIN lessons AS lesson ON lesson.id = quiz.lesson_id
        WHERE quiz.company_id <> lesson.company_id
    ) THEN
        RAISE EXCEPTION 'academy version backfill: quiz tenant mismatch';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM lessons AS lesson
        LEFT JOIN quizzes AS quiz ON quiz.id = lesson.quiz_id
        WHERE lesson.quiz_id IS NOT NULL
          AND (quiz.id IS NULL OR quiz.lesson_id <> lesson.id)
    ) THEN
        RAISE EXCEPTION 'academy version backfill: lesson quiz pointer mismatch';
    END IF;
END
$$;

-- Each legacy course becomes version 1. Reusing legacy content IDs is safe
-- because the version tables are separate and makes the subsequent progress
-- migration lossless: legacy lesson and quiz IDs map directly to version 1.
INSERT INTO course_versions (
    company_id, course_id, number, status, title, description, cover_url,
    sequential, default_internal_deadline_days, created_by_id, created_at,
    published_by_id, published_at
)
SELECT course.company_id,
       course.id,
       1,
       course.status,
       course.title,
       course.description,
       course.cover_url,
       course.sequential,
       course.deadline_days,
       COALESCE(course.created_by_id, course.author_id),
       course.created_at,
       CASE WHEN course.status = 'published' THEN course.author_id END,
       CASE WHEN course.status = 'published' THEN course.updated_at END
FROM courses AS course;

INSERT INTO course_version_sections (
    id, company_id, course_version_id, stable_key, title, "order"
)
SELECT section.id,
       version.company_id,
       version.id,
       section.id,
       section.title,
       section."order"
FROM course_sections AS section
JOIN course_versions AS version
  ON version.course_id = section.course_id
 AND version.number = 1;

INSERT INTO course_version_lessons (
    id, company_id, course_version_id, section_version_id, stable_key,
    title, "order", content, source_type, source_article_id
)
SELECT lesson.id,
       version.company_id,
       version.id,
       lesson.section_id,
       lesson.id,
       lesson.title,
       lesson."order",
       lesson.content,
       CASE
           WHEN lesson.source_article_id IS NULL THEN 'manual'
           WHEN lesson.source_mode = 'link' THEN 'kb_link'
           ELSE 'kb_snapshot'
       END,
       lesson.source_article_id
FROM lessons AS lesson
JOIN course_versions AS version
  ON version.course_id = lesson.course_id
 AND version.number = 1;

INSERT INTO course_version_quizzes (
    id, company_id, course_version_id, lesson_version_id,
    questions, passing_score, max_attempts
)
SELECT quiz.id,
       version_lesson.company_id,
       version_lesson.course_version_id,
       version_lesson.id,
       quiz.questions,
       quiz.passing_score,
       quiz.max_attempts
FROM quizzes AS quiz
JOIN course_version_lessons AS version_lesson
  ON version_lesson.id = quiz.lesson_id;

UPDATE course_version_lessons AS lesson
SET quiz_version_id = quiz.id
FROM course_version_quizzes AS quiz
WHERE quiz.lesson_version_id = lesson.id
  AND quiz.course_version_id = lesson.course_version_id;

-- Hash only snapshot data and stable keys, never version-row IDs, so cloned
-- identical content has the same digest. jsonb text output is canonical for
-- object key ordering; arrays below have explicit deterministic ordering.
UPDATE course_versions AS version
SET content_hash = encode(digest(jsonb_build_object(
    'title', version.title,
    'description', version.description,
    'coverFileId', version.cover_file_id,
    'coverUrl', version.cover_url,
    'sequential', version.sequential,
    'defaultInternalDeadlineDays', version.default_internal_deadline_days,
    'sections', COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
            'stableKey', section.stable_key,
            'title', section.title,
            'order', section."order"
        ) ORDER BY section."order", section.stable_key)
        FROM course_version_sections AS section
        WHERE section.course_version_id = version.id
    ), '[]'::jsonb),
    'lessons', COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
            'stableKey', lesson.stable_key,
            'sectionStableKey', section.stable_key,
            'title', lesson.title,
            'order', lesson."order",
            'content', lesson.content,
            'sourceType', lesson.source_type,
            'sourceArticleId', lesson.source_article_id,
            'sourceArticleVersion', lesson.source_article_version,
            'sourceTemplateId', lesson.source_template_id,
            'sourceTemplateVersionId', lesson.source_template_version_id,
            'estimatedMinutes', lesson.estimated_minutes
        ) ORDER BY section."order", lesson."order", lesson.stable_key)
        FROM course_version_lessons AS lesson
        JOIN course_version_sections AS section ON section.id = lesson.section_version_id
        WHERE lesson.course_version_id = version.id
    ), '[]'::jsonb),
    'quizzes', COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
            'lessonStableKey', lesson.stable_key,
            'questions', quiz.questions,
            'passingScore', quiz.passing_score,
            'maxAttempts', quiz.max_attempts
        ) ORDER BY lesson.stable_key)
        FROM course_version_quizzes AS quiz
        JOIN course_version_lessons AS lesson ON lesson.id = quiz.lesson_version_id
        WHERE quiz.course_version_id = version.id
    ), '[]'::jsonb)
)::text, 'sha256'), 'hex')
WHERE version.status IN ('published', 'retired');

UPDATE courses AS course
SET current_draft_version_id = CASE
        WHEN version.status = 'draft' THEN version.id
    END,
    latest_published_version_id = CASE
        WHEN version.status = 'published' THEN version.id
    END
FROM course_versions AS version
WHERE version.course_id = course.id
  AND version.number = 1;

ALTER TABLE course_versions
    ADD CONSTRAINT course_versions_publication_metadata_check CHECK (
        (status = 'draft'
            AND published_by_id IS NULL
            AND published_at IS NULL
            AND content_hash IS NULL)
        OR (status IN ('published', 'retired')
            AND published_by_id IS NOT NULL
            AND published_at IS NOT NULL
            AND content_hash ~ '^[0-9a-f]{64}$')
    ) NOT VALID;

ALTER TABLE course_versions
    VALIDATE CONSTRAINT course_versions_publication_metadata_check;

ALTER TABLE courses
    ADD CONSTRAINT courses_current_draft_version_fk
        FOREIGN KEY (company_id, id, current_draft_version_id)
        REFERENCES course_versions (company_id, course_id, id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT courses_latest_published_version_fk
        FOREIGN KEY (company_id, id, latest_published_version_id)
        REFERENCES course_versions (company_id, course_id, id)
        ON DELETE RESTRICT;

CREATE INDEX courses_current_draft_version_idx
    ON courses (current_draft_version_id)
    WHERE current_draft_version_id IS NOT NULL;

CREATE INDEX courses_latest_published_version_idx
    ON courses (latest_published_version_id)
    WHERE latest_published_version_id IS NOT NULL;

-- Published/retired snapshots are immutable even if a future application bug
-- issues a direct UPDATE/DELETE. Draft rows stay editable and disposable.
CREATE FUNCTION academy_guard_course_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status = 'draft'
       AND NEW.status NOT IN ('draft', 'published') THEN
        RAISE EXCEPTION 'invalid course version status transition'
            USING ERRCODE = '55000';
    END IF;

    IF TG_OP = 'DELETE' AND OLD.status <> 'draft' THEN
        RAISE EXCEPTION 'published course version is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF TG_OP = 'UPDATE' AND OLD.status <> 'draft' THEN
        IF to_jsonb(NEW) - 'status' IS DISTINCT FROM to_jsonb(OLD) - 'status'
           OR NOT (OLD.status = 'published' AND NEW.status = 'retired') THEN
            RAISE EXCEPTION 'published course version is immutable'
                USING ERRCODE = '55000';
        END IF;
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER course_versions_immutable_trigger
BEFORE UPDATE OR DELETE ON course_versions
FOR EACH ROW EXECUTE FUNCTION academy_guard_course_version_mutation();

CREATE FUNCTION academy_guard_course_version_content_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    old_snapshot_status text;
    new_snapshot_status text;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        SELECT status INTO old_snapshot_status
        FROM course_versions
        WHERE id = OLD.course_version_id;
    END IF;

    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        SELECT status INTO new_snapshot_status
        FROM course_versions
        WHERE id = NEW.course_version_id;
    END IF;

    IF (TG_OP IN ('UPDATE', 'DELETE') AND old_snapshot_status IS DISTINCT FROM 'draft')
       OR (TG_OP IN ('INSERT', 'UPDATE') AND new_snapshot_status IS DISTINCT FROM 'draft') THEN
        RAISE EXCEPTION 'published course version content is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER course_version_sections_immutable_trigger
BEFORE INSERT OR UPDATE OR DELETE ON course_version_sections
FOR EACH ROW EXECUTE FUNCTION academy_guard_course_version_content_mutation();

CREATE TRIGGER course_version_lessons_immutable_trigger
BEFORE INSERT OR UPDATE OR DELETE ON course_version_lessons
FOR EACH ROW EXECUTE FUNCTION academy_guard_course_version_content_mutation();

CREATE TRIGGER course_version_quizzes_immutable_trigger
BEFORE INSERT OR UPDATE OR DELETE ON course_version_quizzes
FOR EACH ROW EXECUTE FUNCTION academy_guard_course_version_content_mutation();
