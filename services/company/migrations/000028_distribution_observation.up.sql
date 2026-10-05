ALTER TABLE distribution_rules ADD COLUMN execution_mode text NOT NULL DEFAULT 'live' CHECK(execution_mode IN('live','observe')), ADD COLUMN execution_epoch bigint NOT NULL DEFAULT 1 CHECK(execution_epoch BETWEEN 1 AND 9007199254740991), ADD COLUMN live_started_at timestamptz;
UPDATE distribution_rules SET live_started_at=first_activation_at;
ALTER TABLE distribution_queue ADD COLUMN execution_epoch bigint NOT NULL DEFAULT 1 CHECK(execution_epoch BETWEEN 1 AND 9007199254740991);
CREATE TABLE distribution_observation_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),company_id uuid NOT NULL,rule_id uuid NOT NULL REFERENCES distribution_rules(id),execution_epoch bigint NOT NULL,entry_id uuid NOT NULL REFERENCES distribution_observed_entries(id),event_id uuid NOT NULL,binding_id uuid NOT NULL,binding_revision bigint NOT NULL,account_id text NOT NULL,lead_id text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','processing','completed')),attempts int NOT NULL DEFAULT 0 CHECK(attempts>=0),next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),lease_token uuid,lease_until timestamptz,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(rule_id,execution_epoch,entry_id,event_id),CHECK((lease_token IS NULL)=(lease_until IS NULL)),CHECK((state='processing')=(lease_token IS NOT NULL)));
CREATE INDEX distribution_observation_job_due ON distribution_observation_jobs(next_attempt_at,created_at,id) WHERE state<>'completed';
CREATE TABLE distribution_observations (
 id uuid PRIMARY KEY REFERENCES distribution_observation_jobs(id),company_id uuid NOT NULL,rule_id uuid NOT NULL REFERENCES distribution_rules(id),group_id uuid NOT NULL,execution_epoch bigint NOT NULL,entry_id uuid NOT NULL,event_id uuid NOT NULL,binding_id uuid NOT NULL,binding_revision bigint NOT NULL,account_id text NOT NULL,lead_id text NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE INDEX distribution_observation_read ON distribution_observations(company_id,rule_id,created_at DESC,id DESC);
CREATE TRIGGER distribution_observations_immutable BEFORE UPDATE OR DELETE ON distribution_observations FOR EACH ROW EXECUTE FUNCTION distribution_history_immutable();

-- Older compatible writers establish the same first live boundary. Observation
-- never receives a live floor merely because active=true.
CREATE FUNCTION distribution_initial_live_floor() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.execution_mode='live' AND NEW.live_started_at IS NULL AND NEW.first_activation_at IS NOT NULL THEN NEW.live_started_at:=NEW.first_activation_at; END IF;
 RETURN NEW; END $$;
CREATE TRIGGER distribution_initial_live_floor_trigger BEFORE INSERT OR UPDATE ON distribution_rules FOR EACH ROW EXECUTE FUNCTION distribution_initial_live_floor();
