DO $$ BEGIN IF EXISTS(SELECT 1 FROM distribution_ui_actions) THEN RAISE EXCEPTION 'Cannot remove used distribution UI action identities'; END IF; END $$;
DROP TABLE distribution_ui_actions;
DROP INDEX distribution_queue_company_group_time;
DROP TRIGGER distribution_group_revision ON distribution_groups;
DROP FUNCTION distribution_group_revision();
ALTER TABLE distribution_groups DROP COLUMN revision;

ALTER TABLE distribution_rules DROP CONSTRAINT distribution_rule_one_group;
