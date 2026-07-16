ALTER TABLE companies
    ADD COLUMN amo_account_id text DEFAULT '31355990'
        CHECK (amo_account_id IS NULL OR amo_account_id ~ '^[0-9]+$');

ALTER TABLE users
    ADD COLUMN source text NOT NULL DEFAULT 'local'
        CHECK (source IN ('local', 'amo')),
    ADD COLUMN external_id text,
    ADD COLUMN external_group_id text,
    ADD COLUMN external_group_name text,
    ADD CONSTRAINT users_external_identity_check CHECK (
        (source = 'local' AND external_id IS NULL)
        OR (source = 'amo' AND external_id IS NOT NULL AND btrim(external_id) <> '')
    );

CREATE UNIQUE INDEX users_company_amo_external_id_uidx
    ON users (company_id, external_id)
    WHERE source = 'amo';
