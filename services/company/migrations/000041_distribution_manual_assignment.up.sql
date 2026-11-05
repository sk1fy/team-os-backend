ALTER TABLE distribution_queue
 ADD COLUMN manual_observation_id uuid UNIQUE REFERENCES distribution_observations(id),
 ADD COLUMN manual_request_id uuid,
 ADD COLUMN manual_employee_id uuid,
 ADD COLUMN manual_actor_id uuid,
 ADD COLUMN manual_expected_responsible_user_id bigint,
 ADD CONSTRAINT distribution_manual_assignment_identity CHECK (
  (manual_observation_id IS NULL AND manual_request_id IS NULL AND manual_employee_id IS NULL
   AND manual_actor_id IS NULL AND manual_expected_responsible_user_id IS NULL)
  OR
  (manual_observation_id IS NOT NULL AND manual_request_id IS NOT NULL AND manual_employee_id IS NOT NULL
   AND manual_actor_id IS NOT NULL AND manual_expected_responsible_user_id IS NOT NULL
   AND manual_expected_responsible_user_id > 0)
 ),
 ADD CONSTRAINT distribution_manual_request_unique UNIQUE (company_id, manual_request_id);

ALTER TABLE distribution_queue DROP CONSTRAINT distribution_queue_entry_id_key;
CREATE UNIQUE INDEX distribution_queue_entry_id_key ON distribution_queue(entry_id)
 WHERE manual_observation_id IS NULL OR NOT settled OR state NOT IN ('failed', 'cancelled');
