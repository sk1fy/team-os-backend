DROP INDEX IF EXISTS sections_public_idx;
ALTER TABLE sections DROP COLUMN IF EXISTS visibility;
