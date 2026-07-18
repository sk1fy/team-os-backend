ALTER TABLE courses
    ADD COLUMN visibility text NOT NULL DEFAULT 'restricted'
        CHECK (visibility IN ('public', 'company', 'restricted'));

WITH duplicates AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY company_id, course_id, assignee_type, assignee_id
               ORDER BY created_at, id
           ) AS duplicate_number
    FROM assignments
    WHERE assignee_type IN ('user', 'position', 'department')
)
DELETE FROM assignments
USING duplicates
WHERE assignments.id = duplicates.id AND duplicates.duplicate_number > 1;

WITH duplicates AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY company_id, course_id, assignee_type
               ORDER BY created_at, id
           ) AS duplicate_number
    FROM assignments
    WHERE assignee_type = 'external'
)
DELETE FROM assignments
USING duplicates
WHERE assignments.id = duplicates.id AND duplicates.duplicate_number > 1;

CREATE UNIQUE INDEX assignments_recipient_uidx
    ON assignments (company_id, course_id, assignee_type, assignee_id)
    WHERE assignee_type IN ('user', 'position', 'department');

CREATE UNIQUE INDEX assignments_external_uidx
    ON assignments (company_id, course_id, assignee_type)
    WHERE assignee_type = 'external';
