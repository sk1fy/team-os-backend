ALTER TABLE positions
    DROP CONSTRAINT positions_level_check;

UPDATE positions
SET level = level + 1;

ALTER TABLE positions
    ALTER COLUMN level SET DEFAULT 1,
    ADD CONSTRAINT positions_level_check CHECK (level BETWEEN 1 AND 5);
