-- New enrollment attempts are identified by enrollment_id/quiz_version_id.
-- Keep the original quiz_id/user_id pair only as an optional compatibility
-- projection for v1 employee attempts; external learners and later versions
-- do not have valid rows in the legacy quizzes/users model.
ALTER TABLE quiz_attempts
    ALTER COLUMN quiz_id DROP NOT NULL,
    ALTER COLUMN user_id DROP NOT NULL,
    ADD CONSTRAINT quiz_attempts_legacy_quiz_binding_check CHECK (
        (quiz_id IS NULL AND user_id IS NULL)
        OR (quiz_id = quiz_version_id AND user_id IS NOT NULL)
    );

-- Validate the tenant, pinned version and learner identity in one place for
-- inserts and subsequent review updates. External identity remains owned by
-- the enrollment instead of being written into the legacy user_id column.
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
          AND (
              (NEW.quiz_id IS NULL AND NEW.user_id IS NULL)
              OR
              (enrollment.learner_type = 'user'
                  AND NEW.quiz_id = quiz.id
                  AND NEW.user_id IS NOT DISTINCT FROM enrollment.user_id
                  AND EXISTS (
                      SELECT 1 FROM quizzes AS legacy_quiz
                      WHERE legacy_quiz.company_id = NEW.company_id
                        AND legacy_quiz.id = NEW.quiz_id
                  ))
          )
    ) THEN
        RAISE EXCEPTION 'quiz attempt does not belong to enrollment learner or course version'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;
