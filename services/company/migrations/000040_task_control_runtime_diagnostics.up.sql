CREATE TABLE task_control_runtime_issued (
 company_id uuid NOT NULL,binding_id uuid NOT NULL,mode_epoch bigint NOT NULL,actor_id text NOT NULL,issue_key uuid NOT NULL,
 publication_id uuid NOT NULL,publication_version bigint NOT NULL,runtime_revision bigint NOT NULL,payload_checksum text NOT NULL,
 issued_at timestamptz NOT NULL,valid_until timestamptz NOT NULL,
 PRIMARY KEY(company_id,binding_id,mode_epoch,actor_id,issue_key),
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE INDEX task_control_runtime_issued_expiry ON task_control_runtime_issued(valid_until);
CREATE TABLE task_control_runtime_events (
 sequence bigserial NOT NULL,company_id uuid NOT NULL,binding_id uuid NOT NULL,event_id uuid NOT NULL,mode_epoch bigint NOT NULL,actor_id text NOT NULL,runtime_instance_id uuid NOT NULL,
 publication_id uuid NOT NULL,runtime_revision bigint NOT NULL,event text NOT NULL CHECK(event IN ('received','applied','heartbeat','error')),
 payload_hash text NOT NULL,payload jsonb NOT NULL,received_at timestamptz NOT NULL,
 PRIMARY KEY(company_id,binding_id,event_id),
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE INDEX task_control_runtime_events_instances ON task_control_runtime_events(company_id,binding_id,mode_epoch,runtime_instance_id,received_at DESC);
CREATE INDEX task_control_runtime_events_expiry ON task_control_runtime_events(received_at);
CREATE INDEX task_control_runtime_events_binding_time ON task_control_runtime_events(company_id,binding_id,received_at DESC);
CREATE INDEX task_control_runtime_events_actor_instance ON task_control_runtime_events(company_id,binding_id,runtime_instance_id,received_at DESC);
