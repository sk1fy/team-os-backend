-- Reuse already confirmed company-local identity data, independently of the
-- Distribution grant/installation. No rights or product activation are inherited.
CREATE VIEW task_control_confirmed_employee_mappings AS
 SELECT company_id,account_id,employee_id,crm_user_id FROM task_control_employee_mappings
 UNION
 SELECT m.company_id,b.account_id,m.user_id AS employee_id,m.crm_user_id
 FROM distribution_employee_mappings m
 JOIN distribution_bindings b ON b.company_id=m.company_id AND b.id=m.binding_id
 WHERE m.state='verified' AND m.user_id IS NOT NULL
 AND b.account_id::numeric<=9007199254740991 AND m.crm_user_id::numeric<=9007199254740991;

CREATE FUNCTION task_control_bump_shared_mapping_revision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cid uuid;
BEGIN
 IF TG_OP='DELETE' THEN cid:=OLD.company_id; ELSE cid:=NEW.company_id; END IF;
 INSERT INTO task_control_reference_revisions(company_id) VALUES(cid) ON CONFLICT DO NOTHING;
 UPDATE task_control_reference_revisions SET mapping_revision=mapping_revision+1 WHERE company_id=cid;
 RETURN NULL;
END $$;
CREATE TRIGGER task_control_shared_mapping_revision AFTER INSERT OR UPDATE OR DELETE ON distribution_employee_mappings
 FOR EACH ROW EXECUTE FUNCTION task_control_bump_shared_mapping_revision();
CREATE TRIGGER task_control_shared_mapping_account_revision AFTER UPDATE OF account_id ON distribution_bindings
 FOR EACH ROW EXECUTE FUNCTION task_control_bump_shared_mapping_revision();
