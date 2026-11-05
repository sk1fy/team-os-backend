ALTER TABLE task_control_connection_requests
    DROP CONSTRAINT task_control_connection_requests_action_check,
    ADD CONSTRAINT task_control_connection_requests_action_check
        CHECK(action IN ('revoke', 'mode', 'activateV2', 'modeV2', 'applyRules'));

CREATE INDEX task_control_applied_rules_binding
    ON task_control_connection_requests (company_id, binding_id)
    WHERE action = 'applyRules';
