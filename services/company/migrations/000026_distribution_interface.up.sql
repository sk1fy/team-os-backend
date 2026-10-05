DO $$ BEGIN IF EXISTS(SELECT 1 FROM distribution_rules GROUP BY company_id,group_id HAVING count(*)>1) THEN RAISE EXCEPTION 'Distribution v1 allows one rule per company/group; resolve duplicate group rules explicitly before migration 26; no history was changed'; END IF; END $$;
ALTER TABLE distribution_rules ADD CONSTRAINT distribution_rule_one_group UNIQUE(company_id,group_id);
ALTER TABLE distribution_groups ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK(revision BETWEEN 1 AND 9007199254740991);
CREATE FUNCTION distribution_group_revision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.revision=OLD.revision+1; RETURN NEW; END $$;
CREATE TRIGGER distribution_group_revision BEFORE UPDATE ON distribution_groups FOR EACH ROW EXECUTE FUNCTION distribution_group_revision();
CREATE TABLE distribution_ui_actions(company_id uuid NOT NULL REFERENCES companies(id),request_id uuid NOT NULL,queue_id uuid NOT NULL REFERENCES distribution_queue(id),actor_id uuid NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(company_id,request_id));
CREATE TRIGGER distribution_ui_actions_nochange BEFORE UPDATE OR DELETE ON distribution_ui_actions FOR EACH ROW EXECUTE FUNCTION distribution_history_immutable();
CREATE INDEX distribution_queue_company_group_time ON distribution_queue(company_id,group_id,created_at,id);
