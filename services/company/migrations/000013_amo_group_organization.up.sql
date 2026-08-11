ALTER TABLE departments
    ADD COLUMN source text NOT NULL DEFAULT 'local'
        CHECK (source IN ('local', 'amo')),
    ADD COLUMN external_id text,
    ADD CONSTRAINT departments_external_identity_check CHECK (
        (source = 'local' AND external_id IS NULL)
        OR (source = 'amo' AND external_id IS NOT NULL AND btrim(external_id) <> '')
    );

CREATE UNIQUE INDEX departments_company_amo_external_id_uidx
    ON departments (company_id, external_id)
    WHERE source = 'amo';

CREATE TABLE user_departments (
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    department_id uuid NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, department_id),
    UNIQUE (user_id)
);

CREATE INDEX user_departments_department_idx ON user_departments (department_id);
