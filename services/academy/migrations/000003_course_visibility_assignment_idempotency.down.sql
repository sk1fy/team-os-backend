DROP INDEX IF EXISTS assignments_external_uidx;
DROP INDEX IF EXISTS assignments_recipient_uidx;
ALTER TABLE courses DROP COLUMN IF EXISTS visibility;
