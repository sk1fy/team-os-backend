-- Account-qualified mappings contain only proven CRM identities. Historical
-- source='amo' rows are deliberately NOT backfilled: their importing account
-- was not recorded and may have changed since import.
CREATE TABLE task_control_reference_revisions (
 company_id uuid PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
 organization_revision bigint NOT NULL DEFAULT 1 CHECK(organization_revision > 0),
 mapping_revision bigint NOT NULL DEFAULT 1 CHECK(mapping_revision > 0)
);
INSERT INTO task_control_reference_revisions(company_id) SELECT id FROM companies;
CREATE TABLE task_control_employee_mappings (
 company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
 account_id text NOT NULL CHECK(account_id ~ '^[1-9][0-9]{0,15}$'),
 employee_id uuid NOT NULL,
 crm_user_id text NOT NULL CHECK(crm_user_id ~ '^[1-9][0-9]{0,15}$'),
 PRIMARY KEY(company_id,account_id,employee_id,crm_user_id),
 FOREIGN KEY(company_id,employee_id) REFERENCES users(company_id,id) ON DELETE CASCADE
);
CREATE FUNCTION task_control_bump_reference_revision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cid uuid;
BEGIN
 IF TG_OP='DELETE' THEN cid:=OLD.company_id; ELSE cid:=NEW.company_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM companies WHERE id=cid) THEN RETURN NULL; END IF;
 INSERT INTO task_control_reference_revisions(company_id) VALUES(cid) ON CONFLICT DO NOTHING;
 IF TG_TABLE_NAME='task_control_employee_mappings' THEN
  UPDATE task_control_reference_revisions SET mapping_revision=mapping_revision+1 WHERE company_id=cid;
 ELSE
  UPDATE task_control_reference_revisions SET organization_revision=organization_revision+1 WHERE company_id=cid;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER task_control_org_users AFTER INSERT OR UPDATE OR DELETE ON users FOR EACH ROW EXECUTE FUNCTION task_control_bump_reference_revision();
CREATE TRIGGER task_control_org_departments AFTER INSERT OR UPDATE OR DELETE ON departments FOR EACH ROW EXECUTE FUNCTION task_control_bump_reference_revision();
CREATE TRIGGER task_control_org_positions AFTER INSERT OR UPDATE OR DELETE ON positions FOR EACH ROW EXECUTE FUNCTION task_control_bump_reference_revision();
CREATE TRIGGER task_control_org_user_positions AFTER INSERT OR UPDATE OR DELETE ON user_positions FOR EACH ROW EXECUTE FUNCTION task_control_bump_reference_revision();
CREATE TRIGGER task_control_org_user_departments AFTER INSERT OR UPDATE OR DELETE ON user_departments FOR EACH ROW EXECUTE FUNCTION task_control_bump_reference_revision();
CREATE TRIGGER task_control_mapping_revision AFTER INSERT OR UPDATE OR DELETE ON task_control_employee_mappings FOR EACH ROW EXECUTE FUNCTION task_control_bump_reference_revision();
CREATE TABLE task_control_reference_cache (
 company_id uuid NOT NULL, binding_id uuid NOT NULL, scope_key text NOT NULL,
 payload jsonb, last_error text, updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(company_id,binding_id,scope_key),
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id) ON DELETE CASCADE
);
CREATE TABLE task_control_reference_refreshes (
 id uuid PRIMARY KEY, company_id uuid NOT NULL, binding_id uuid NOT NULL, scope_key text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','completed','failed')),
 error_code text, created_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz,
 lease_until timestamptz,
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX task_control_reference_refresh_coalesce ON task_control_reference_refreshes(company_id,binding_id,scope_key) WHERE state IN ('pending','running');
CREATE INDEX task_control_reference_refresh_queue ON task_control_reference_refreshes(created_at) WHERE state IN ('pending','running');
