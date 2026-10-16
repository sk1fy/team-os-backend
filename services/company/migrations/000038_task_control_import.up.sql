CREATE TABLE task_control_import_objects (
 company_id uuid NOT NULL REFERENCES companies(id), binding_id uuid NOT NULL,
 id uuid NOT NULL, kind text NOT NULL CHECK(kind IN ('source','preview','apply')),
 payload jsonb NOT NULL, PRIMARY KEY(company_id,binding_id,kind,id),
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE INDEX task_control_import_source_idx ON task_control_import_objects(company_id,binding_id) WHERE kind='source';
