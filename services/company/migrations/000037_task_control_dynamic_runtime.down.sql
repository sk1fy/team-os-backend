-- V2 intent cannot be reinterpreted by the v1 schema after dropping its marker.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM task_control_connections WHERE runtime_schema_version=2)
 OR EXISTS(SELECT 1 FROM task_control_drafts WHERE schema_version=2)
 OR EXISTS(SELECT 1 FROM task_control_publications WHERE payload->>'schemaVersion'='2') THEN
  RAISE EXCEPTION 'Task Control v2 data exists: preserve migration 37 and use an explicit intent migration before schema rollback';
 END IF;
END $$;
DROP TABLE task_control_v2_failures;
DROP TRIGGER task_control_v2_source_changed ON task_control_reference_revisions;
DROP FUNCTION task_control_v2_source_changed();
DROP TABLE task_control_v2_source_changes;
DELETE FROM task_control_connection_requests WHERE action IN ('activateV2','modeV2');
ALTER TABLE task_control_connection_requests DROP CONSTRAINT task_control_connection_requests_action_check;
ALTER TABLE task_control_connection_requests ADD CONSTRAINT task_control_connection_requests_action_check CHECK(action IN ('revoke','mode'));
DROP TABLE task_control_v2_jobs,task_control_v2_acks,task_control_v2_issued,task_control_v2_snapshots,task_control_v2_state;
ALTER TABLE task_control_drafts DROP COLUMN schema_version;
ALTER TABLE task_control_connections DROP COLUMN runtime_schema_version;
