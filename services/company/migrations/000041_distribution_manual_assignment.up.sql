ALTER TABLE distribution_queue
 ADD COLUMN manual_observation_id uuid UNIQUE REFERENCES distribution_observations(id),
 ADD COLUMN manual_request_id uuid,
 ADD CONSTRAINT distribution_manual_assignment_identity CHECK ((manual_observation_id IS NULL) = (manual_request_id IS NULL)),
 ADD CONSTRAINT distribution_manual_request_unique UNIQUE (company_id, manual_request_id);
