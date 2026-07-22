DROP TRIGGER IF EXISTS assignments_reject_new_external_trigger ON assignments;
DROP FUNCTION IF EXISTS academy_reject_new_external_assignment();

DROP TRIGGER IF EXISTS quiz_attempts_enrollment_scope_trigger ON quiz_attempts;
DROP FUNCTION IF EXISTS academy_validate_quiz_attempt_scope();

DROP TRIGGER IF EXISTS enrollment_lesson_progress_scope_trigger ON enrollment_lesson_progress;
DROP FUNCTION IF EXISTS academy_validate_enrollment_lesson_progress_scope();

DROP INDEX IF EXISTS quiz_attempts_company_pending_review_idx;
DROP INDEX IF EXISTS quiz_attempts_enrollment_quiz_created_idx;

ALTER TABLE quiz_attempts
    DROP CONSTRAINT IF EXISTS quiz_attempts_review_metadata_check,
    DROP CONSTRAINT IF EXISTS quiz_attempts_version_quiz_fk,
    DROP CONSTRAINT IF EXISTS quiz_attempts_enrollment_fk,
    DROP COLUMN IF EXISTS review_comment,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS reviewed_by_id,
    DROP COLUMN IF EXISTS answers,
    DROP COLUMN IF EXISTS quiz_version_id,
    DROP COLUMN IF EXISTS enrollment_id;

DROP TABLE IF EXISTS enrollment_lesson_progress;
DROP TABLE IF EXISTS course_enrollments;

ALTER TABLE course_version_quizzes
    DROP CONSTRAINT IF EXISTS course_version_quizzes_company_id_key;

ALTER TABLE course_version_lessons
    DROP CONSTRAINT IF EXISTS course_version_lessons_company_id_key;

DROP INDEX IF EXISTS assignments_company_version_idx;

ALTER TABLE assignments
    DROP CONSTRAINT IF EXISTS assignments_course_version_fk,
    DROP COLUMN IF EXISTS course_version_id;
