DROP TRIGGER IF EXISTS article_snapshot_reuse_grants_immutable_trigger
    ON article_snapshot_reuse_grants;
DROP FUNCTION IF EXISTS kb_preserve_article_snapshot_reuse_grant();
DROP TRIGGER IF EXISTS article_snapshot_reuse_grants_validate_trigger
    ON article_snapshot_reuse_grants;
DROP FUNCTION IF EXISTS kb_validate_article_snapshot_reuse_grant();
DROP TABLE IF EXISTS article_snapshot_reuse_grants;
DROP TABLE IF EXISTS article_partner_access_grants;

ALTER TABLE acknowledgements
    DROP CONSTRAINT IF EXISTS acknowledgements_article_tenant_fk;
ALTER TABLE acknowledgements
    ADD CONSTRAINT acknowledgements_article_id_fkey
        FOREIGN KEY (article_id) REFERENCES articles (id) ON DELETE CASCADE;

ALTER TABLE article_versions
    DROP CONSTRAINT IF EXISTS article_versions_article_tenant_fk,
    DROP CONSTRAINT IF EXISTS article_versions_company_id_id_key,
    DROP CONSTRAINT IF EXISTS article_versions_company_article_id_key;
ALTER TABLE article_versions
    ADD CONSTRAINT article_versions_article_id_fkey
        FOREIGN KEY (article_id) REFERENCES articles (id) ON DELETE CASCADE;

ALTER TABLE articles
    DROP CONSTRAINT IF EXISTS articles_section_tenant_fk,
    DROP COLUMN IF EXISTS partner_reuse_policy,
    DROP COLUMN IF EXISTS partner_access_mode,
    DROP CONSTRAINT IF EXISTS articles_company_id_id_key;
ALTER TABLE articles
    ADD CONSTRAINT articles_section_id_fkey
        FOREIGN KEY (section_id) REFERENCES sections (id) ON DELETE RESTRICT;

ALTER TABLE sections DROP CONSTRAINT IF EXISTS sections_parent_tenant_fk;
ALTER TABLE sections
    DROP CONSTRAINT IF EXISTS sections_company_id_id_key,
    ADD CONSTRAINT sections_parent_id_fkey
        FOREIGN KEY (parent_id) REFERENCES sections (id) ON DELETE RESTRICT;
