-- Older binaries require a manually configured unique active point. Rollback must
-- first pause trigger rules and restore their legacy point configuration.
DO $$ BEGIN IF EXISTS(SELECT 1 FROM distribution_rules WHERE active AND source='digital_pipeline' AND (pipeline_id='' OR status_id='')) THEN RAISE EXCEPTION 'Pause active Digital Pipeline rules and restore their legacy pipeline/status before rolling back migration 30'; END IF; END $$;
DROP INDEX distribution_rule_active_trigger_group;
DROP INDEX distribution_rule_active_point;
CREATE UNIQUE INDEX distribution_rule_active_point ON distribution_rules(account_id,pipeline_id,status_id) WHERE active;
DROP TRIGGER distribution_binding_wake ON distribution_bindings;
CREATE TRIGGER distribution_binding_wake AFTER UPDATE ON distribution_bindings FOR EACH ROW EXECUTE FUNCTION distribution_queue_wake();
ALTER TABLE distribution_bindings DROP COLUMN timezone_fetched_at, DROP COLUMN account_timezone, DROP COLUMN account_domain;
DROP INDEX distribution_queue_deadline_pending;
DROP TRIGGER distribution_queue_deadline_on_insert ON distribution_queue;
DROP FUNCTION distribution_queue_deadline();
ALTER TABLE distribution_queue DROP COLUMN next_shift_at, DROP COLUMN waiting_deadline_at;
