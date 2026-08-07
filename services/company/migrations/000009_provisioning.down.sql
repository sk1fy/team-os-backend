DROP TABLE IF EXISTS provisioning_requests;
DROP TABLE IF EXISTS sso_tokens;
DROP TABLE IF EXISTS bootstrap_activations;
DROP TABLE IF EXISTS user_external_identities;
DROP TABLE IF EXISTS company_integrations;

ALTER TABLE companies
    DROP CONSTRAINT IF EXISTS companies_owner_fk,
    ADD CONSTRAINT companies_owner_fk
        FOREIGN KEY (owner_id) REFERENCES users(id)
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_company_id_id_unique,
    DROP CONSTRAINT IF EXISTS users_external_identity_check,
    DROP CONSTRAINT IF EXISTS users_source_check;

UPDATE users SET source = 'local', external_id = NULL WHERE source = 'external';

ALTER TABLE users
    ADD CONSTRAINT users_source_check CHECK (source IN ('local', 'amo')),
    ADD CONSTRAINT users_external_identity_check CHECK (
        (source = 'local' AND external_id IS NULL)
        OR (source = 'amo' AND external_id IS NOT NULL AND btrim(external_id) <> '')
    );

ALTER TABLE companies
    DROP CONSTRAINT IF EXISTS companies_onboarding_lifecycle_check,
    DROP COLUMN IF EXISTS onboarding_completed_at,
    DROP COLUMN IF EXISTS status;

ALTER TABLE companies
    ALTER COLUMN amo_account_id SET DEFAULT '31355990';
