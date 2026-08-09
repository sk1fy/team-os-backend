CREATE TABLE company_registration_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    provider text NOT NULL
        CHECK (provider ~ '^[a-z][a-z0-9_-]{1,31}$'),
    external_account_id text NOT NULL
        CHECK (
            external_account_id = btrim(external_account_id)
            AND external_account_id ~ '^[0-9]+$'
            AND length(external_account_id) <= 255
        ),
    token_hash bytea NOT NULL UNIQUE
        CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    revocation_reason text CHECK (
        revocation_reason IS NULL
        OR revocation_reason IN ('expired', 'reissued', 'cleanup')
    ),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (
        (revoked_at IS NULL AND revocation_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revocation_reason IS NOT NULL)
    ),
    CHECK (NOT (consumed_at IS NOT NULL AND revoked_at IS NOT NULL))
);

CREATE UNIQUE INDEX company_registration_tokens_account_active_uidx
    ON company_registration_tokens (provider, external_account_id)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE INDEX company_registration_tokens_expiry_idx
    ON company_registration_tokens (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;
