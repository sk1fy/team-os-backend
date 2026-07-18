ALTER TABLE sections
    ADD COLUMN visibility text NOT NULL DEFAULT 'company'
        CHECK (visibility IN ('public', 'company'));

CREATE INDEX sections_public_idx ON sections (id) WHERE visibility = 'public';
