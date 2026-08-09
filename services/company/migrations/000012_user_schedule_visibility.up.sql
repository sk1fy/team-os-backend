ALTER TABLE users
    ADD COLUMN show_in_schedule boolean NOT NULL DEFAULT true;

UPDATE users
SET show_in_schedule = false
WHERE role = 'owner';
