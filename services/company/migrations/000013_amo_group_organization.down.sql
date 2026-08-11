DROP TABLE IF EXISTS user_departments;

DROP INDEX IF EXISTS departments_company_amo_external_id_uidx;

ALTER TABLE departments
    DROP CONSTRAINT IF EXISTS departments_external_identity_check,
    DROP COLUMN IF EXISTS external_id,
    DROP COLUMN IF EXISTS source;
