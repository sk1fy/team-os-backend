CREATE TABLE notification_users (
  company_id uuid NOT NULL,
  user_id uuid NOT NULL,
  active boolean NOT NULL,
  position_ids uuid[] NOT NULL DEFAULT '{}',
  department_ids uuid[] NOT NULL DEFAULT '{}',
  last_event_at timestamptz NOT NULL,
  PRIMARY KEY (company_id, user_id)
);

CREATE INDEX notification_users_position_ids_idx ON notification_users USING gin (position_ids);
CREATE INDEX notification_users_department_ids_idx ON notification_users USING gin (department_ids);

INSERT INTO notification_users (company_id, user_id, active, last_event_at)
SELECT company_id, user_id, true, max(created_at)
FROM notifications
GROUP BY company_id, user_id
ON CONFLICT DO NOTHING;
