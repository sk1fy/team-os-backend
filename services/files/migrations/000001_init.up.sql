CREATE TABLE files (
  id uuid PRIMARY KEY,
  company_id uuid NOT NULL,
  uploaded_by uuid NOT NULL,
  object_key text NOT NULL UNIQUE,
  name text NOT NULL,
  content_type text NOT NULL,
  size bigint NOT NULL CHECK (size > 0),
  purpose text NOT NULL CHECK (purpose IN ('attachment', 'avatar', 'logo')),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX files_company_created_idx ON files (company_id, created_at DESC);
