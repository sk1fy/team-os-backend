-- The old default silently linked every newly-created company to the Rakurs
-- demo account. Keep the legacy column during the rolling migration because
-- old application instances still read it, but require all new writes to be
-- explicit.
ALTER TABLE companies
    ALTER COLUMN amo_account_id DROP DEFAULT,
    ADD COLUMN status text NOT NULL DEFAULT 'active'
        CHECK (status IN ('onboarding', 'active', 'frozen', 'suspended')),
    ADD COLUMN onboarding_completed_at timestamptz DEFAULT now();

UPDATE companies
SET onboarding_completed_at = created_at
WHERE status <> 'onboarding';

ALTER TABLE companies
    ADD CONSTRAINT companies_onboarding_lifecycle_check CHECK (
        (status = 'onboarding' AND onboarding_completed_at IS NULL)
        OR
        (status IN ('active', 'frozen', 'suspended')
            AND onboarding_completed_at IS NOT NULL)
    );

-- Composite keys let the new tables enforce company isolation in their
-- foreign keys instead of relying on application-side checks alone.
ALTER TABLE users
    DROP CONSTRAINT users_source_check,
    DROP CONSTRAINT users_external_identity_check,
    ADD CONSTRAINT users_source_check
        CHECK (source IN ('local', 'amo', 'external')),
    ADD CONSTRAINT users_external_identity_check CHECK (
        (source IN ('local', 'external') AND external_id IS NULL)
        OR (source = 'amo' AND external_id IS NOT NULL AND btrim(external_id) <> '')
    ),
    ADD CONSTRAINT users_company_id_id_unique UNIQUE (company_id, id);

-- A company owner must belong to that same company. The original single-column
-- foreign key only guaranteed that the user existed somewhere in the system.
ALTER TABLE companies
    DROP CONSTRAINT companies_owner_fk,
    ADD CONSTRAINT companies_owner_fk
        FOREIGN KEY (id, owner_id) REFERENCES users(company_id, id)
        DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE company_integrations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    provider text NOT NULL
        CHECK (provider ~ '^[a-z][a-z0-9_-]{1,31}$'),
    external_account_id text NOT NULL
        CHECK (
            external_account_id = btrim(external_account_id)
            AND external_account_id <> ''
            AND length(external_account_id) <= 255
        ),
    app_name text CHECK (
        app_name IS NULL OR (
            app_name = btrim(app_name)
            AND app_name <> ''
            AND length(app_name) <= 255
        )
    ),
    entitlements text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'frozen', 'suspended')),
    last_verified_at timestamptz,
    frozen_at timestamptz,
    metadata jsonb NOT NULL DEFAULT '{}'
        CHECK (jsonb_typeof(metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, external_account_id),
    UNIQUE (company_id, provider),
    UNIQUE (company_id, id),
    UNIQUE (company_id, id, provider, external_account_id)
);

CREATE INDEX company_integrations_company_status_idx
    ON company_integrations (company_id, status);

CREATE TABLE user_external_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    integration_id uuid NOT NULL,
    user_id uuid NOT NULL,
    provider text NOT NULL,
    external_account_id text NOT NULL,
    external_user_id text NOT NULL
        CHECK (
            external_user_id = btrim(external_user_id)
            AND external_user_id <> ''
            AND length(external_user_id) <= 255
        ),
    status text NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'deactivated')),
    last_verified_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (company_id, integration_id, provider, external_account_id)
        REFERENCES company_integrations(company_id, id, provider, external_account_id)
        ON DELETE CASCADE,
    FOREIGN KEY (company_id, user_id)
        REFERENCES users(company_id, id) ON DELETE CASCADE,
    UNIQUE (company_id, provider, external_user_id),
    UNIQUE (company_id, user_id, provider),
    UNIQUE (company_id, user_id, id)
);

CREATE INDEX user_external_identities_company_user_idx
    ON user_external_identities (company_id, user_id);

CREATE TABLE bootstrap_activations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL CHECK (role IN ('owner', 'admin')),
    purpose text NOT NULL CHECK (purpose IN ('initiator', 'second_user')),
    token_hash bytea NOT NULL UNIQUE
        CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    revocation_reason text CHECK (
        revocation_reason IS NULL
        OR revocation_reason IN ('reissued', 'provisioning_replayed', 'expired')
    ),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (company_id, user_id)
        REFERENCES users(company_id, id) ON DELETE CASCADE,
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (
        (revoked_at IS NULL AND revocation_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revocation_reason IS NOT NULL)
    ),
    CHECK (NOT (consumed_at IS NOT NULL AND revoked_at IS NOT NULL))
);

CREATE UNIQUE INDEX bootstrap_activations_user_active_uidx
    ON bootstrap_activations (company_id, user_id)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE INDEX bootstrap_activations_expiry_idx
    ON bootstrap_activations (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE TABLE sso_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    user_id uuid NOT NULL,
    external_identity_id uuid NOT NULL,
    purpose text NOT NULL DEFAULT 'login' CHECK (purpose = 'login'),
    token_hash bytea NOT NULL UNIQUE
        CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (company_id, user_id, external_identity_id)
        REFERENCES user_external_identities(company_id, user_id, id) ON DELETE CASCADE,
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE UNIQUE INDEX sso_tokens_identity_active_uidx
    ON sso_tokens (external_identity_id)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE INDEX sso_tokens_expiry_idx
    ON sso_tokens (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE TABLE provisioning_requests (
    provider text NOT NULL
        CHECK (provider ~ '^[a-z][a-z0-9_-]{1,31}$'),
    idempotency_key text NOT NULL
        CHECK (
            idempotency_key = btrim(idempotency_key)
            AND idempotency_key <> ''
            AND length(idempotency_key) <= 255
        ),
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    external_account_id text NOT NULL,
    company_id uuid NOT NULL,
    integration_id uuid NOT NULL,
    initiator_user_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (provider, idempotency_key),
    FOREIGN KEY (company_id, integration_id, provider, external_account_id)
        REFERENCES company_integrations(company_id, id, provider, external_account_id)
        ON DELETE CASCADE,
    FOREIGN KEY (company_id, initiator_user_id)
        REFERENCES users(company_id, id) ON DELETE CASCADE,
    CHECK (expires_at > created_at)
);

CREATE INDEX provisioning_requests_expiry_idx
    ON provisioning_requests (expires_at);
