-- Partner access is deny-by-default and belongs to KB. Academy receives only
-- a checked immutable snapshot through gRPC and never reads these tables.
ALTER TABLE sections
    ADD CONSTRAINT sections_company_id_id_key UNIQUE (company_id, id);

ALTER TABLE sections DROP CONSTRAINT sections_parent_id_fkey;
ALTER TABLE sections
    ADD CONSTRAINT sections_parent_tenant_fk
        FOREIGN KEY (company_id, parent_id)
        REFERENCES sections (company_id, id) ON DELETE RESTRICT;

ALTER TABLE articles
    ADD CONSTRAINT articles_company_id_id_key UNIQUE (company_id, id),
    ADD COLUMN partner_access_mode text NOT NULL DEFAULT 'none'
        CHECK (partner_access_mode IN ('none', 'all', 'selected')),
    ADD COLUMN partner_reuse_policy text NOT NULL DEFAULT 'not_allowed'
        CHECK (partner_reuse_policy IN ('not_allowed', 'copy_allowed'));

ALTER TABLE articles DROP CONSTRAINT articles_section_id_fkey;
ALTER TABLE articles
    ADD CONSTRAINT articles_section_tenant_fk
        FOREIGN KEY (company_id, section_id)
        REFERENCES sections (company_id, id) ON DELETE RESTRICT;

ALTER TABLE article_versions
    ADD CONSTRAINT article_versions_company_article_id_key
        UNIQUE (company_id, article_id, id),
    ADD CONSTRAINT article_versions_company_id_id_key UNIQUE (company_id, id);

ALTER TABLE article_versions DROP CONSTRAINT article_versions_article_id_fkey;
ALTER TABLE article_versions
    ADD CONSTRAINT article_versions_article_tenant_fk
        FOREIGN KEY (company_id, article_id)
        REFERENCES articles (company_id, id) ON DELETE CASCADE;

ALTER TABLE acknowledgements DROP CONSTRAINT acknowledgements_article_id_fkey;
ALTER TABLE acknowledgements
    ADD CONSTRAINT acknowledgements_article_tenant_fk
        FOREIGN KEY (company_id, article_id)
        REFERENCES articles (company_id, id) ON DELETE CASCADE;

CREATE TABLE article_partner_access_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    article_id uuid NOT NULL,
    partner_id uuid NOT NULL,
    granted_by_id uuid NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT article_partner_access_grants_article_fk
        FOREIGN KEY (company_id, article_id)
        REFERENCES articles (company_id, id) ON DELETE CASCADE,
    CONSTRAINT article_partner_access_grants_partner_key
        UNIQUE (company_id, article_id, partner_id)
);

CREATE INDEX article_partner_access_grants_partner_idx
    ON article_partner_access_grants (company_id, partner_id, article_id);

-- A successful grant is immutable evidence of the exact published version
-- returned to Academy. Revoking access/policy blocks new grants but never
-- mutates snapshots already copied into courses.
CREATE TABLE article_snapshot_reuse_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    article_id uuid NOT NULL,
    article_version_id uuid NOT NULL,
    article_version integer NOT NULL CHECK (article_version >= 1),
    partner_id uuid NOT NULL,
    requested_by_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (
        btrim(idempotency_key) <> '' AND octet_length(idempotency_key) <= 512
    ),
    content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    source_file_ids uuid[] NOT NULL DEFAULT '{}'::uuid[],
    granted_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT article_snapshot_reuse_grants_version_fk
        FOREIGN KEY (company_id, article_id, article_version_id)
        REFERENCES article_versions (company_id, article_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT article_snapshot_reuse_grants_idempotency_key
        UNIQUE (company_id, article_id, article_version_id, partner_id, idempotency_key)
);

CREATE INDEX article_snapshot_reuse_grants_partner_idx
    ON article_snapshot_reuse_grants (
        company_id, partner_id, granted_at DESC, id
    );

CREATE FUNCTION kb_validate_article_snapshot_reuse_grant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    current_status text;
    current_version integer;
    access_mode text;
    reuse_policy text;
    version_number integer;
BEGIN
    SELECT article.status, article.version, article.partner_access_mode,
           article.partner_reuse_policy, version.version
    INTO current_status, current_version, access_mode, reuse_policy,
         version_number
    FROM articles AS article
    JOIN article_versions AS version
      ON version.company_id = article.company_id
     AND version.article_id = article.id
     AND version.id = NEW.article_version_id
    WHERE article.company_id = NEW.company_id
      AND article.id = NEW.article_id;

    IF NOT FOUND OR current_status <> 'published'
       OR current_version <> version_number
       OR NEW.article_version <> version_number THEN
        RAISE EXCEPTION 'KB reuse: only current published version can be copied';
    END IF;
    IF reuse_policy <> 'copy_allowed' THEN
        RAISE EXCEPTION 'KB reuse: article copying is not allowed';
    END IF;
    IF access_mode = 'none'
       OR (access_mode = 'selected' AND NOT EXISTS (
            SELECT 1 FROM article_partner_access_grants AS access_grant
            WHERE access_grant.company_id = NEW.company_id
              AND access_grant.article_id = NEW.article_id
              AND access_grant.partner_id = NEW.partner_id
       )) THEN
        RAISE EXCEPTION 'KB reuse: partner has no article access';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER article_snapshot_reuse_grants_validate_trigger
BEFORE INSERT ON article_snapshot_reuse_grants
FOR EACH ROW EXECUTE FUNCTION kb_validate_article_snapshot_reuse_grant();

CREATE FUNCTION kb_preserve_article_snapshot_reuse_grant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'KB reuse grant is immutable' USING ERRCODE = '55000';
END
$$;

CREATE TRIGGER article_snapshot_reuse_grants_immutable_trigger
BEFORE UPDATE OR DELETE ON article_snapshot_reuse_grants
FOR EACH ROW EXECUTE FUNCTION kb_preserve_article_snapshot_reuse_grant();
