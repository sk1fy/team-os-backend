ALTER TABLE task_control_connections ADD COLUMN runtime_schema_version integer NOT NULL DEFAULT 1 CHECK(runtime_schema_version IN (1,2));
ALTER TABLE task_control_drafts ADD COLUMN schema_version integer NOT NULL DEFAULT 1 CHECK(schema_version IN (1,2));
CREATE TABLE task_control_v2_state (
 company_id uuid NOT NULL, binding_id uuid NOT NULL, current_snapshot_id uuid, runtime_revision bigint NOT NULL DEFAULT 0 CHECK(runtime_revision>=0 AND runtime_revision<=9007199254740991),
 verified_source_fresh_until timestamptz, PRIMARY KEY(company_id,binding_id),
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE TABLE task_control_v2_snapshots (
 company_id uuid NOT NULL,binding_id uuid NOT NULL,id uuid NOT NULL,mode_epoch bigint NOT NULL,runtime_revision bigint NOT NULL,publication_id uuid NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(company_id,id),UNIQUE(company_id,binding_id,mode_epoch,runtime_revision),FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE INDEX task_control_v2_snapshots_expiry ON task_control_v2_snapshots(created_at);
CREATE TABLE task_control_v2_issued (
 company_id uuid NOT NULL,binding_id uuid NOT NULL,mode_epoch bigint NOT NULL,actor_id text NOT NULL,snapshot_id uuid NOT NULL,payload_checksum text NOT NULL,payload jsonb NOT NULL,issued_at timestamptz NOT NULL,
 PRIMARY KEY(company_id,binding_id,mode_epoch,actor_id,snapshot_id),FOREIGN KEY(company_id,snapshot_id) REFERENCES task_control_v2_snapshots(company_id,id)
);
CREATE TABLE task_control_v2_acks (
 company_id uuid NOT NULL,binding_id uuid NOT NULL,mode_epoch bigint NOT NULL,actor_id text NOT NULL,runtime_instance_id uuid NOT NULL,runtime_revision bigint NOT NULL,payload_checksum text NOT NULL,payload jsonb NOT NULL,received_at timestamptz NOT NULL,
 PRIMARY KEY(company_id,binding_id,mode_epoch,actor_id,runtime_instance_id,runtime_revision),FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE TABLE task_control_v2_jobs (
 company_id uuid NOT NULL,binding_id uuid NOT NULL,mode_epoch bigint NOT NULL,publication_id uuid NOT NULL,queued_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(company_id,binding_id),FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
ALTER TABLE task_control_connection_requests DROP CONSTRAINT task_control_connection_requests_action_check;
ALTER TABLE task_control_connection_requests ADD CONSTRAINT task_control_connection_requests_action_check CHECK(action IN ('revoke','mode','activateV2','modeV2'));

-- Append-only source notifications avoid a revision->job lock inversion. Source
-- transactions never wait for a resolver holding an existing job row.
CREATE TABLE task_control_v2_source_changes (
 id bigserial PRIMARY KEY,
 company_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION task_control_v2_source_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO task_control_v2_source_changes(company_id) VALUES(NEW.company_id);
 RETURN NEW;
END $$;
CREATE TRIGGER task_control_v2_source_changed AFTER INSERT OR UPDATE ON task_control_reference_revisions
 FOR EACH ROW EXECUTE FUNCTION task_control_v2_source_changed();
CREATE TABLE task_control_v2_failures(company_id uuid NOT NULL,binding_id uuid NOT NULL,code text NOT NULL,message text NOT NULL,updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(company_id,binding_id),FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id));
