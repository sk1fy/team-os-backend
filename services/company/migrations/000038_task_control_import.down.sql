-- Import originals and draft backups are durable migration evidence.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM task_control_import_objects) THEN
  RAISE EXCEPTION 'Task Control import originals/backups exist: preserve or explicitly archive them before schema rollback';
 END IF;
END $$;
DROP TABLE task_control_import_objects;
