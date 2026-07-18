DROP TABLE IF EXISTS employee_access_audit;
ALTER TABLE users DROP COLUMN IF EXISTS avatar_source;
UPDATE users SET last_name = '' WHERE last_name IS NULL;
ALTER TABLE users ALTER COLUMN last_name SET NOT NULL;
