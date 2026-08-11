DROP TRIGGER IF EXISTS companies_create_system_department ON companies;
DROP FUNCTION IF EXISTS create_company_system_department();

UPDATE departments AS root
SET "order" = COALESCE((
        SELECT max(child."order") + 1
        FROM departments AS child
        WHERE child.company_id = root.company_id
          AND child.parent_id = root.id
    ), 0),
    updated_at = now()
WHERE root.source = 'system';

UPDATE departments AS department
SET parent_id = NULL,
    updated_at = now()
FROM departments AS root
WHERE root.company_id = department.company_id
  AND root.source = 'system'
  AND department.parent_id = root.id;

DELETE FROM departments WHERE source = 'system';

DROP INDEX IF EXISTS departments_company_system_root_uidx;

ALTER TABLE departments
    DROP CONSTRAINT departments_source_check,
    DROP CONSTRAINT departments_external_identity_check,
    ADD CONSTRAINT departments_source_check
        CHECK (source IN ('local', 'amo')),
    ADD CONSTRAINT departments_external_identity_check CHECK (
        (source = 'local' AND external_id IS NULL)
        OR (source = 'amo' AND external_id IS NOT NULL AND btrim(external_id) <> '')
    );
