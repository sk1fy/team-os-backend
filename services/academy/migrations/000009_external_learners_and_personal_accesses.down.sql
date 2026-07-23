DROP TRIGGER IF EXISTS course_enrollment_access_history_immutable_trigger
    ON course_enrollment_access_history;
DROP TRIGGER IF EXISTS external_personal_access_history_immutable_trigger
    ON external_personal_access_history;
DROP FUNCTION IF EXISTS academy_preserve_external_history();

DROP TABLE IF EXISTS course_enrollment_access_history;
DROP TABLE IF EXISTS external_personal_access_history;
DROP TABLE IF EXISTS external_mutation_idempotency;

DROP TRIGGER IF EXISTS external_personal_accesses_validate_trigger
    ON external_personal_accesses;
DROP FUNCTION IF EXISTS academy_validate_external_personal_access();

DROP INDEX IF EXISTS course_enrollments_external_personal_source_uidx;

DROP TABLE IF EXISTS external_personal_accesses;
DROP TABLE IF EXISTS external_sessions;
DROP TABLE IF EXISTS external_verification_challenges;
DROP TABLE IF EXISTS external_learners;
