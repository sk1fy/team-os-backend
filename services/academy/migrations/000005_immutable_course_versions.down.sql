DROP TRIGGER IF EXISTS course_version_quizzes_immutable_trigger ON course_version_quizzes;
DROP TRIGGER IF EXISTS course_version_lessons_immutable_trigger ON course_version_lessons;
DROP TRIGGER IF EXISTS course_version_sections_immutable_trigger ON course_version_sections;
DROP TRIGGER IF EXISTS course_versions_immutable_trigger ON course_versions;

DROP FUNCTION IF EXISTS academy_guard_course_version_content_mutation();
DROP FUNCTION IF EXISTS academy_guard_course_version_mutation();

DROP INDEX IF EXISTS courses_latest_published_version_idx;
DROP INDEX IF EXISTS courses_current_draft_version_idx;

ALTER TABLE courses
    DROP CONSTRAINT IF EXISTS courses_latest_published_version_fk,
    DROP CONSTRAINT IF EXISTS courses_current_draft_version_fk;

DROP TABLE IF EXISTS course_version_publish_idempotency;

ALTER TABLE course_version_lessons
    DROP CONSTRAINT IF EXISTS course_version_lessons_quiz_fk;

DROP TABLE IF EXISTS course_version_quizzes;
DROP TABLE IF EXISTS course_version_lessons;
DROP TABLE IF EXISTS course_version_sections;
DROP TABLE IF EXISTS course_versions;

ALTER TABLE courses
    DROP CONSTRAINT IF EXISTS courses_company_id_id_key,
    DROP COLUMN IF EXISTS latest_published_version_id,
    DROP COLUMN IF EXISTS current_draft_version_id;
