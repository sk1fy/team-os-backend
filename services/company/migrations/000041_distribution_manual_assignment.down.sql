DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM distribution_queue WHERE manual_observation_id IS NOT NULL) THEN
  RAISE EXCEPTION 'Manual assignment identities must be retained';
 END IF;
END $$;
ALTER TABLE distribution_queue DROP COLUMN manual_observation_id, DROP COLUMN manual_request_id,
 DROP COLUMN manual_employee_id, DROP COLUMN manual_actor_id, DROP COLUMN manual_expected_responsible_user_id;
