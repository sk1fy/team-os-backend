ALTER TABLE user_admin_audit
    DROP CONSTRAINT user_admin_audit_action_check,
    ADD CONSTRAINT user_admin_audit_action_check CHECK (action IN (
        'sections_changed', 'deactivated', 'reactivated', 'deleted',
        'external_removed', 'external_restored', 'amo_admin_self_login'
    ));
