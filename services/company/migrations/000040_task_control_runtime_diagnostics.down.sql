DO $$ BEGIN IF EXISTS(SELECT 1 FROM task_control_runtime_events) THEN RAISE EXCEPTION 'Retained runtime evidence prevents downgrade'; END IF; END $$;
DROP TABLE task_control_runtime_events;
DROP TABLE task_control_runtime_issued;
