-- Internal Academy commands that return a one-time public token must replay
-- the first response after an HTTP timeout. The raw token is derived from the
-- server secret and is never persisted; this table stores only the response
-- snapshot and the request binding.

CREATE TABLE external_token_mutation_idempotency (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    operation text NOT NULL CHECK (
        operation IN (
            'create_personal_access',
            'rotate_personal_access_token',
            'repeat_personal_access',
            'create_external_campaign',
            'rotate_external_campaign_token'
        )
    ),
    idempotency_key text NOT NULL CHECK (
        btrim(idempotency_key) <> ''
        AND octet_length(idempotency_key) BETWEEN 8 AND 255
    ),
    request_hash text NOT NULL CHECK (
        request_hash ~ '^[0-9a-f]{64}$'
    ),
    result_id uuid NOT NULL,
    response_payload jsonb,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_token_mutation_idempotency_company_id_id_key
        UNIQUE (company_id, id),
    CONSTRAINT external_token_mutation_idempotency_key
        UNIQUE (company_id, actor_user_id, operation, idempotency_key),
    CONSTRAINT external_token_mutation_idempotency_completion_check CHECK (
        (response_payload IS NULL AND completed_at IS NULL)
        OR
        (response_payload IS NOT NULL AND completed_at IS NOT NULL
            AND completed_at >= created_at)
    )
);

CREATE INDEX external_token_mutation_idempotency_result_idx
    ON external_token_mutation_idempotency (
        company_id, operation, result_id
    );
