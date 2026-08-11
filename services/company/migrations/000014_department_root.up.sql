ALTER TABLE departments
    DROP CONSTRAINT departments_source_check,
    DROP CONSTRAINT departments_external_identity_check,
    ADD CONSTRAINT departments_source_check
        CHECK (source IN ('local', 'amo', 'system')),
    ADD CONSTRAINT departments_external_identity_check CHECK (
        (source IN ('local', 'system') AND external_id IS NULL)
        OR (source = 'amo' AND external_id IS NOT NULL AND btrim(external_id) <> '')
    );

CREATE UNIQUE INDEX departments_company_system_root_uidx
    ON departments (company_id)
    WHERE source = 'system';

INSERT INTO departments (id, company_id, name, parent_id, "order", source)
SELECT gen_random_uuid(), company.id, company.name, NULL,
       COALESCE((
           SELECT max(department."order") + 1
           FROM departments AS department
           WHERE department.company_id = company.id
             AND department.parent_id IS NULL
       ), 0),
       'system'
FROM companies AS company;

UPDATE departments AS department
SET parent_id = root.id,
    updated_at = now()
FROM departments AS root
WHERE root.company_id = department.company_id
  AND root.source = 'system'
  AND department.source <> 'system'
  AND department.parent_id IS NULL;

UPDATE departments
SET "order" = 0,
    updated_at = now()
WHERE source = 'system';

CREATE FUNCTION create_company_system_department() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO departments (id, company_id, name, parent_id, "order", source)
    VALUES (gen_random_uuid(), NEW.id, NEW.name, NULL, 0, 'system');
    RETURN NEW;
END;
$$;

CREATE TRIGGER companies_create_system_department
AFTER INSERT ON companies
FOR EACH ROW EXECUTE FUNCTION create_company_system_department();
