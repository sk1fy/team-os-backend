CREATE TABLE file_clone_operations (
  id uuid PRIMARY KEY,
  company_id uuid NOT NULL,
  idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
  requested_by uuid NOT NULL,
  target_owner_type text NOT NULL CHECK (target_owner_type IN ('course_version', 'template_version', 'article_version')),
  target_owner_id uuid NOT NULL,
  source_file_ids uuid[] NOT NULL CHECK (cardinality(source_file_ids) BETWEEN 1 AND 100),
  state text NOT NULL CHECK (state IN ('pending', 'in_progress', 'succeeded', 'failed')),
  error_message text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (company_id, idempotency_key)
);

CREATE TABLE file_clones (
  operation_id uuid NOT NULL REFERENCES file_clone_operations(id) ON DELETE CASCADE,
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  source_file_id uuid NOT NULL,
  target_file_id uuid NOT NULL REFERENCES files(id) ON DELETE RESTRICT,
  PRIMARY KEY (operation_id, source_file_id),
  UNIQUE (operation_id, ordinal),
  UNIQUE (target_file_id)
);

CREATE INDEX file_clone_operations_company_updated_idx
  ON file_clone_operations (company_id, updated_at DESC);

CREATE INDEX file_clones_source_idx ON file_clones (source_file_id);
