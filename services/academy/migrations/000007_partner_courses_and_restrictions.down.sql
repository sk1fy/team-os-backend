DROP INDEX IF EXISTS course_enrollments_course_access_idx;

ALTER TABLE course_enrollments
    DROP CONSTRAINT IF EXISTS course_enrollments_restriction_suspension_shape_check,
    DROP CONSTRAINT IF EXISTS course_enrollments_restriction_previous_access_check,
    DROP COLUMN IF EXISTS restriction_previous_access_status;

DROP TABLE IF EXISTS partner_course_copy_idempotency;

DROP TRIGGER IF EXISTS course_origins_preserve_trigger ON course_origins;
DROP FUNCTION IF EXISTS academy_preserve_course_origin();
DROP TRIGGER IF EXISTS course_origins_validate_trigger ON course_origins;
DROP FUNCTION IF EXISTS academy_validate_course_origin();
DROP TABLE IF EXISTS course_origins;

DROP TRIGGER IF EXISTS course_restrictions_preserve_history_trigger ON course_restrictions;
DROP FUNCTION IF EXISTS academy_preserve_course_restriction_history();
DROP TRIGGER IF EXISTS course_restrictions_validate_target_trigger ON course_restrictions;
DROP FUNCTION IF EXISTS academy_validate_course_restriction_target();
DROP TABLE IF EXISTS course_restrictions;
