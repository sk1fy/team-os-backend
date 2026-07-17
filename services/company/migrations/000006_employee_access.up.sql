CREATE TABLE access_links (
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
