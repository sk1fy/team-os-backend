CREATE TABLE task_control_connections (
 company_id uuid NOT NULL REFERENCES companies(id), id uuid NOT NULL,
 installation_id uuid NOT NULL, integration_id uuid NOT NULL, account_id text NOT NULL CHECK(account_id ~ '^[1-9][0-9]{0,15}$'),
 generation bigint NOT NULL CHECK(generation>0), binding_revision bigint NOT NULL DEFAULT 1,
 mode_epoch bigint NOT NULL DEFAULT 1, mode text NOT NULL DEFAULT 'standalone' CHECK(mode IN ('standalone','managed')),
 state text NOT NULL DEFAULT 'active' CHECK(state IN ('active','revoked')),
 intent_id uuid NOT NULL, linked_by uuid NOT NULL, crm_admin_id text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(company_id,id), UNIQUE(company_id,intent_id)
);
CREATE UNIQUE INDEX task_control_active_installation ON task_control_connections(installation_id) WHERE state='active';
CREATE UNIQUE INDEX task_control_active_account ON task_control_connections(company_id,account_id) WHERE state='active';
CREATE TABLE task_control_installation_snapshots (
 company_id uuid NOT NULL REFERENCES companies(id), account_id text NOT NULL,
 payload jsonb NOT NULL, checked_at timestamptz NOT NULL, fresh_until timestamptz NOT NULL,
 PRIMARY KEY(company_id,account_id)
);
CREATE TABLE task_control_permissions (
 company_id uuid NOT NULL, user_id uuid NOT NULL, can_read boolean NOT NULL, can_manage boolean NOT NULL,
 PRIMARY KEY(company_id,user_id), FOREIGN KEY(company_id,user_id) REFERENCES users(company_id,id),
 CHECK(NOT can_manage OR can_read)
);
CREATE TABLE task_control_assertion_uses (
 key_id text NOT NULL, jti uuid NOT NULL, company_id uuid NOT NULL REFERENCES companies(id), expires_at timestamptz NOT NULL,
 PRIMARY KEY(key_id,jti)
);
CREATE TABLE task_control_runtime_sessions (
 token_hash bytea PRIMARY KEY CHECK(octet_length(token_hash)=32), company_id uuid NOT NULL, binding_id uuid NOT NULL,
 actor_id text NOT NULL, binding_revision bigint NOT NULL, mode_epoch bigint NOT NULL,
 generation bigint NOT NULL, expires_at timestamptz NOT NULL,
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE TABLE task_control_connection_requests (
 company_id uuid NOT NULL, request_id uuid NOT NULL, binding_id uuid NOT NULL, actor_id uuid NOT NULL,
 action text NOT NULL, body_hash text NOT NULL, response jsonb NOT NULL,
 PRIMARY KEY(company_id,request_id), FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
