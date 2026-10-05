ALTER TABLE distribution_service_grants DROP CONSTRAINT distribution_service_grants_capability_check;
ALTER TABLE distribution_service_grants ADD CONSTRAINT distribution_service_grants_capability_check
 CHECK(capability IN ('widget-access','decision-validation','event-delivery','result-delivery'));
CREATE TABLE distribution_delivery_inbox (
 receipt_id uuid PRIMARY KEY, consumer_id text NOT NULL, message_kind text NOT NULL CHECK(message_kind IN ('event','result')),
 message_id uuid NOT NULL, event_id uuid NOT NULL, company_id uuid NOT NULL, binding_id uuid NOT NULL, binding_revision bigint NOT NULL CHECK(binding_revision BETWEEN 1 AND 9007199254740991),
 installation_id uuid NOT NULL, integration_id uuid NOT NULL, account_id text NOT NULL, lead_id text NOT NULL,
 payload_hash bytea NOT NULL CHECK(octet_length(payload_hash)=32), payload jsonb NOT NULL,
 state text NOT NULL DEFAULT 'received' CHECK(state IN ('received','processing','applied','ignored','blocked')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0), next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_token uuid, lease_until timestamptz, observation_generation bigint, error_code text,
 accepted_at timestamptz NOT NULL DEFAULT now(), applied_at timestamptz,
 CHECK((lease_token IS NULL)=(lease_until IS NULL)), CHECK((state='processing')=(lease_token IS NOT NULL)), CHECK(observation_generation IS NULL OR observation_generation BETWEEN 1 AND 9007199254740991), UNIQUE(consumer_id,message_kind,message_id),
 FOREIGN KEY(company_id,binding_id,binding_revision) REFERENCES distribution_binding_versions(company_id,binding_id,revision)
);
CREATE INDEX distribution_delivery_due ON distribution_delivery_inbox(next_attempt_at,accepted_at) WHERE state IN ('received','processing','blocked');
CREATE TABLE distribution_event_receipts (
 consumer_id text NOT NULL,event_id uuid NOT NULL,company_id uuid NOT NULL,
 payload_hash bytea NOT NULL CHECK(octet_length(payload_hash)=32),receipt_id uuid NOT NULL REFERENCES distribution_delivery_inbox(receipt_id),
 PRIMARY KEY(consumer_id,event_id)
);
CREATE TABLE distribution_lead_heads (
 account_id text NOT NULL,lead_id text NOT NULL,company_id uuid NOT NULL REFERENCES companies(id),
 binding_id uuid NOT NULL,binding_revision bigint NOT NULL CHECK(binding_revision BETWEEN 1 AND 9007199254740991),
 next_generation bigint NOT NULL DEFAULT 0 CHECK(next_generation BETWEEN 0 AND 9007199254740991),applied_generation bigint NOT NULL DEFAULT 0 CHECK(applied_generation BETWEEN 0 AND 9007199254740991),
 observation_revision bigint NOT NULL DEFAULT 0 CHECK(observation_revision BETWEEN 0 AND 9007199254740991),last_sequence bigint NOT NULL DEFAULT 0 CHECK(last_sequence BETWEEN 0 AND 9007199254740991),
 snapshot jsonb,deleted boolean NOT NULL DEFAULT false,absent boolean NOT NULL DEFAULT false,absence_reason text,current_entry_id uuid,
 CHECK((absent AND absence_reason='not_found_or_deleted') OR (NOT absent AND absence_reason IS NULL)),
 CHECK(NOT deleted OR NOT absent),
 observed_at timestamptz,updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(account_id,lead_id),UNIQUE(company_id,account_id,lead_id),
 FOREIGN KEY(company_id,binding_id,binding_revision) REFERENCES distribution_binding_versions(company_id,binding_id,revision)
);
CREATE TABLE distribution_observed_entries (
 id uuid PRIMARY KEY,company_id uuid NOT NULL,account_id text NOT NULL,lead_id text NOT NULL,
 binding_id uuid NOT NULL,binding_revision bigint NOT NULL CHECK(binding_revision BETWEEN 1 AND 9007199254740991),sequence bigint NOT NULL CHECK(sequence BETWEEN 1 AND 9007199254740991),
 pipeline_id text NOT NULL,status_id text NOT NULL,entry_event_id uuid NOT NULL,evidence text NOT NULL,
 state text NOT NULL CHECK(state IN ('checking','needs_configuration','cancelled')),
 cancellation_reason text,created_at timestamptz NOT NULL DEFAULT now(),finished_at timestamptz,
 UNIQUE(account_id,lead_id,sequence),
 FOREIGN KEY(company_id,account_id,lead_id) REFERENCES distribution_lead_heads(company_id,account_id,lead_id),
 FOREIGN KEY(company_id,binding_id,binding_revision) REFERENCES distribution_binding_versions(company_id,binding_id,revision)
);
CREATE TABLE distribution_operation_mirrors (
 operation_id uuid PRIMARY KEY,company_id uuid NOT NULL,binding_id uuid NOT NULL,binding_revision bigint NOT NULL CHECK(binding_revision BETWEEN 1 AND 9007199254740991),
 installation_id uuid NOT NULL,integration_id uuid NOT NULL,account_id text NOT NULL,lead_id text NOT NULL,
 episode_id uuid NOT NULL,decision_id uuid NOT NULL,rule_id uuid NOT NULL,group_id uuid NOT NULL,
 target_responsible_id text NOT NULL,result_version bigint NOT NULL DEFAULT 0 CHECK(result_version BETWEEN 0 AND 9007199254740991),
 result_hash bytea,result_payload jsonb,state text NOT NULL DEFAULT 'registered',unfinished boolean NOT NULL DEFAULT true,
 registration_payload jsonb NOT NULL,registered_by uuid NOT NULL,registered_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 reconcile_attempts integer NOT NULL DEFAULT 0 CHECK(reconcile_attempts>=0),next_reconcile_at timestamptz NOT NULL DEFAULT now(),error_code text,
 FOREIGN KEY(company_id,binding_id,binding_revision) REFERENCES distribution_binding_versions(company_id,binding_id,revision)
);
CREATE TABLE distribution_operation_mirror_versions (
 operation_id uuid NOT NULL REFERENCES distribution_operation_mirrors(operation_id),result_version bigint NOT NULL CHECK(result_version BETWEEN 1 AND 9007199254740991),
 payload_hash bytea NOT NULL CHECK(octet_length(payload_hash)=32),payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(operation_id,result_version)
);
