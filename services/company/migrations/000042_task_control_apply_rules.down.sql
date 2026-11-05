DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM task_control_connection_requests WHERE action = 'applyRules') THEN
        RAISE EXCEPTION 'Cannot revert migration 42 after applying TeamOS rules';
    END IF;
END $$;

DROP INDEX task_control_applied_rules_binding;

ALTER TABLE task_control_connection_requests
    DROP CONSTRAINT task_control_connection_requests_action_check,
    ADD CONSTRAINT task_control_connection_requests_action_check
        CHECK(action IN ('revoke', 'mode', 'activateV2', 'modeV2'));
