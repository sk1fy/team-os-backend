DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM distribution_observation_jobs) OR EXISTS(SELECT 1 FROM distribution_rules WHERE execution_mode='observe' OR execution_epoch>1) THEN RAISE EXCEPTION 'Observation identities are in use; keep compatible schema'; END IF;
END $$;
DROP TRIGGER distribution_initial_live_floor_trigger ON distribution_rules;
DROP FUNCTION distribution_initial_live_floor();
DROP TABLE distribution_observations;
DROP TABLE distribution_observation_jobs;
ALTER TABLE distribution_queue DROP COLUMN execution_epoch;
ALTER TABLE distribution_rules DROP COLUMN execution_mode,DROP COLUMN execution_epoch,DROP COLUMN live_started_at;
