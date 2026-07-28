CREATE TABLE employee_section_access (
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    section text NOT NULL CHECK (
        section IN ('schedule', 'knowledge', 'academy', 'distribution')
    ),
    granted_by uuid,
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, section)
);

CREATE INDEX employee_section_access_company_user_idx
    ON employee_section_access (company_id, user_id);

INSERT INTO employee_section_access (company_id, user_id, section)
SELECT users.company_id, users.id, defaults.section
FROM users
CROSS JOIN unnest(ARRAY['schedule', 'knowledge', 'academy']) AS defaults(section)
WHERE users.role = 'employee';

ALTER TABLE users ADD COLUMN external_deleted_at timestamptz;

ALTER TABLE employee_access_audit
    DROP CONSTRAINT employee_access_audit_target_user_id_fkey,
    DROP CONSTRAINT employee_access_audit_actor_user_id_fkey,
    ALTER COLUMN target_user_id DROP NOT NULL,
    ALTER COLUMN actor_user_id DROP NOT NULL,
    ADD CONSTRAINT employee_access_audit_target_user_id_fkey
        FOREIGN KEY (target_user_id) REFERENCES users(id) ON DELETE SET NULL,
    ADD CONSTRAINT employee_access_audit_actor_user_id_fkey
        FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL;

CREATE TABLE user_admin_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    target_user_id uuid,
    actor_user_id uuid,
    actor_kind text NOT NULL CHECK (actor_kind IN ('user', 'amo_sync', 'system')),
    action text NOT NULL CHECK (action IN (
        'sections_changed', 'deactivated', 'reactivated', 'deleted',
        'external_removed', 'external_restored'
    )),
    before_state jsonb NOT NULL DEFAULT '{}',
    after_state jsonb NOT NULL DEFAULT '{}',
    request_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX user_admin_audit_target_idx
    ON user_admin_audit (company_id, target_user_id, created_at DESC);
