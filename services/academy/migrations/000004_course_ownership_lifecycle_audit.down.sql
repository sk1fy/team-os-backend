DROP TABLE IF EXISTS audit_log;

DROP INDEX IF EXISTS courses_company_distribution_lifecycle_idx;
DROP INDEX IF EXISTS courses_company_owner_user_lifecycle_idx;
DROP INDEX IF EXISTS courses_company_owner_lifecycle_created_idx;

ALTER TABLE courses
    DROP CONSTRAINT IF EXISTS courses_deleted_metadata_check,
    DROP CONSTRAINT IF EXISTS courses_archived_metadata_check,
    DROP CONSTRAINT IF EXISTS courses_distribution_status_check,
    DROP CONSTRAINT IF EXISTS courses_lifecycle_status_check,
    DROP CONSTRAINT IF EXISTS courses_owner_shape_check,
    DROP CONSTRAINT IF EXISTS courses_owner_type_check,
    DROP COLUMN IF EXISTS deleted_by_id,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS archived_by_id,
    DROP COLUMN IF EXISTS archived_at,
    DROP COLUMN IF EXISTS distribution_status,
    DROP COLUMN IF EXISTS lifecycle_status,
    DROP COLUMN IF EXISTS created_by_id,
    DROP COLUMN IF EXISTS owner_user_id,
    DROP COLUMN IF EXISTS owner_type;
