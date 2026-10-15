CREATE TABLE task_control_drafts (
 company_id uuid NOT NULL, binding_id uuid NOT NULL, revision bigint NOT NULL DEFAULT 1,
 rules jsonb NOT NULL DEFAULT '[]', checksum text NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(), updated_by uuid NOT NULL,
 PRIMARY KEY(company_id,binding_id), FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE TABLE task_control_publications (
 company_id uuid NOT NULL, binding_id uuid NOT NULL, id uuid NOT NULL, version bigint NOT NULL,
 request_id uuid NOT NULL, body_hash text NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY(company_id,id), UNIQUE(company_id,binding_id,version), UNIQUE(company_id,request_id),
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
ALTER TABLE task_control_connections ADD COLUMN publication_id uuid;
ALTER TABLE task_control_connection_requests DROP CONSTRAINT IF EXISTS task_control_connection_requests_action_check;
ALTER TABLE task_control_connection_requests ADD CONSTRAINT task_control_connection_requests_action_check CHECK(action IN ('revoke','mode'));
