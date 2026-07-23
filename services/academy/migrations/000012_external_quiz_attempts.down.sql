-- Rows without the legacy identity cannot be represented by the pre-000012
-- schema. Remove only those modern attempts before restoring NOT NULL.
DELETE FROM quiz_attempts
WHERE quiz_id IS NULL OR user_id IS NULL;

ALTER TABLE quiz_attempts
    DROP CONSTRAINT quiz_attempts_legacy_quiz_binding_check,
    ALTER COLUMN quiz_id SET NOT NULL,
    ALTER COLUMN user_id SET NOT NULL;

CREATE OR REPLACE FUNCTION academy_validate_quiz_attempt_scope()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM course_enrollments AS enrollment
        JOIN course_version_quizzes AS quiz
          ON quiz.company_id = enrollment.company_id
         AND quiz.course_version_id = enrollment.course_version_id
        WHERE enrollment.company_id = NEW.company_id
          AND enrollment.id = NEW.enrollment_id
          AND quiz.id = NEW.quiz_version_id
    ) THEN
        RAISE EXCEPTION 'quiz does not belong to enrollment course version'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;
