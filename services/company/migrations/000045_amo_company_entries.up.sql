CREATE TABLE amo_company_entries (
    id uuid PRIMARY KEY,
    company_id uuid NOT NULL,
    user_id uuid NOT NULL,
    external_identity_id uuid NOT NULL,
    account_id text NOT NULL CHECK (account_id ~ '^[1-9][0-9]*$'),
    actor_id text NOT NULL CHECK (actor_id ~ '^[1-9][0-9]*$'),
    core_integration_id uuid NOT NULL,
    core_installation_id uuid NOT NULL,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    FOREIGN KEY (company_id, user_id, external_identity_id)
        REFERENCES user_external_identities(company_id, user_id, id) ON DELETE CASCADE,
    CHECK (expires_at = created_at + interval '60 seconds')
);
CREATE INDEX amo_company_entries_expiry_idx ON amo_company_entries(expires_at);
