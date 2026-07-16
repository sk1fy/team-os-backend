DROP INDEX IF EXISTS users_company_amo_external_id_uidx;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_external_identity_check,
    DROP COLUMN IF EXISTS external_group_name,
    DROP COLUMN IF EXISTS external_group_id,
    DROP COLUMN IF EXISTS external_id,
    DROP COLUMN IF EXISTS source;

ALTER TABLE companies
    DROP COLUMN IF EXISTS amo_account_id;
