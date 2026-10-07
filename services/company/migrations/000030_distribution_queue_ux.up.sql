ALTER TABLE distribution_queue ADD COLUMN waiting_deadline_at timestamptz;
UPDATE distribution_queue SET waiting_deadline_at=created_at+interval '72 hours';
ALTER TABLE distribution_queue ALTER COLUMN waiting_deadline_at SET NOT NULL;
CREATE FUNCTION distribution_queue_deadline() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 NEW.waiting_deadline_at := NEW.created_at+interval '72 hours'; RETURN NEW; END $$;
CREATE TRIGGER distribution_queue_deadline_on_insert BEFORE INSERT ON distribution_queue FOR EACH ROW EXECUTE FUNCTION distribution_queue_deadline();
ALTER TABLE distribution_queue ADD COLUMN next_shift_at timestamptz;
CREATE INDEX distribution_queue_deadline_pending ON distribution_queue(waiting_deadline_at,id) WHERE NOT settled;
ALTER TABLE distribution_bindings ADD COLUMN account_timezone text, ADD COLUMN timezone_fetched_at timestamptz, ADD COLUMN account_domain text;
-- Authenticated DP rules are selected by account/binding and group, not a manually configured point.
DROP INDEX distribution_rule_active_point;
CREATE UNIQUE INDEX distribution_rule_active_point ON distribution_rules(account_id,pipeline_id,status_id) WHERE active AND source<>'digital_pipeline';
CREATE UNIQUE INDEX distribution_rule_active_trigger_group ON distribution_rules(company_id,binding_id,group_id) WHERE active AND source='digital_pipeline';
DROP TRIGGER distribution_binding_wake ON distribution_bindings;
CREATE TRIGGER distribution_binding_wake AFTER UPDATE ON distribution_bindings FOR EACH ROW WHEN ((OLD.state,OLD.revision,OLD.mapping_revision,OLD.mapping_ack_revision,OLD.account_timezone) IS DISTINCT FROM (NEW.state,NEW.revision,NEW.mapping_revision,NEW.mapping_ack_revision,NEW.account_timezone)) EXECUTE FUNCTION distribution_queue_wake();
