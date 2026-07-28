DROP TABLE IF EXISTS user_admin_audit;

ALTER TABLE employee_access_audit
    DROP CONSTRAINT employee_access_audit_target_user_id_fkey,
    DROP CONSTRAINT employee_access_audit_actor_user_id_fkey;

DELETE FROM employee_access_audit
WHERE target_user_id IS NULL OR actor_user_id IS NULL;

ALTER TABLE employee_access_audit
    ALTER COLUMN target_user_id SET NOT NULL,
    ALTER COLUMN actor_user_id SET NOT NULL,
    ADD CONSTRAINT employee_access_audit_target_user_id_fkey
        FOREIGN KEY (target_user_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT employee_access_audit_actor_user_id_fkey
        FOREIGN KEY (actor_user_id) REFERENCES users(id);

ALTER TABLE users DROP COLUMN IF EXISTS external_deleted_at;
DROP TABLE IF EXISTS employee_section_access;
