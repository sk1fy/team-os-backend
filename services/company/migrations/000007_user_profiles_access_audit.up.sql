ALTER TABLE users
    ALTER COLUMN last_name DROP NOT NULL,
    ADD COLUMN avatar_source text
        CHECK (avatar_source IS NULL OR avatar_source IN ('amo'));

UPDATE users
SET avatar_source = 'amo'
WHERE source = 'amo' AND avatar_url IS NOT NULL;

CREATE TABLE employee_access_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    target_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_user_id uuid NOT NULL REFERENCES users(id),
    action text NOT NULL CHECK (action IN ('issued', 'reissued', 'revoked')),
    mode text NOT NULL CHECK (mode IN ('none', 'password', 'link')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX employee_access_audit_target_idx
    ON employee_access_audit (company_id, target_user_id, created_at DESC);
