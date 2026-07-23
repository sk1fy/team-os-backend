-- name: UpdateArticlePartnerAccessMode :one
UPDATE articles
SET partner_access_mode = sqlc.arg(partner_access_mode), updated_at = sqlc.arg(updated_at)
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id)
RETURNING id, company_id, section_id, title, content, status, author_id,
    version, requires_acknowledgement, plain_text, partner_access_mode,
    partner_reuse_policy, created_at, updated_at;

-- name: UpdateArticlePartnerReusePolicy :one
UPDATE articles
SET partner_reuse_policy = sqlc.arg(partner_reuse_policy),
    updated_at = sqlc.arg(updated_at)
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id)
RETURNING id, company_id, section_id, title, content, status, author_id,
    version, requires_acknowledgement, plain_text, partner_access_mode,
    partner_reuse_policy, created_at, updated_at;

-- name: DeleteArticlePartnerAccessGrants :execrows
DELETE FROM article_partner_access_grants
WHERE company_id = sqlc.arg(company_id) AND article_id = sqlc.arg(article_id);

-- name: CreateArticlePartnerAccessGrants :many
INSERT INTO article_partner_access_grants (
    id, company_id, article_id, partner_id, granted_by_id, granted_at
)
SELECT gen_random_uuid(), article.company_id, article.id, partner_id,
    sqlc.arg(granted_by_id), sqlc.arg(granted_at)
FROM articles AS article
CROSS JOIN unnest(sqlc.arg(partner_ids)::uuid[]) AS partner_id
WHERE article.company_id = sqlc.arg(company_id)
  AND article.id = sqlc.arg(article_id)
  AND article.partner_access_mode = 'selected'
ON CONFLICT (company_id, article_id, partner_id) DO UPDATE
SET granted_by_id = EXCLUDED.granted_by_id,
    granted_at = EXCLUDED.granted_at
RETURNING id, company_id, article_id, partner_id, granted_by_id, granted_at;

-- name: ListArticlePartnerAccessGrants :many
SELECT id, company_id, article_id, partner_id, granted_by_id, granted_at
FROM article_partner_access_grants
WHERE company_id = sqlc.arg(company_id) AND article_id = sqlc.arg(article_id)
ORDER BY partner_id;

-- name: GetPartnerArticle :one
SELECT article.id, article.company_id, article.section_id, article.title,
    article.content, article.status, article.author_id, article.version,
    article.requires_acknowledgement, article.plain_text,
    article.partner_access_mode, article.partner_reuse_policy,
    article.created_at, article.updated_at
FROM articles AS article
WHERE article.company_id = sqlc.arg(company_id)
  AND article.id = sqlc.arg(id)
  AND article.status = 'published'
  AND (
      article.partner_access_mode = 'all'
      OR (
          article.partner_access_mode = 'selected'
          AND EXISTS (
              SELECT 1 FROM article_partner_access_grants AS access_grant
              WHERE access_grant.company_id = article.company_id
                AND access_grant.article_id = article.id
                AND access_grant.partner_id = sqlc.arg(partner_id)
          )
      )
  );

-- name: ListPartnerArticles :many
SELECT article.id, article.company_id, article.section_id, article.title,
    article.content, article.status, article.author_id, article.version,
    article.requires_acknowledgement, article.plain_text,
    article.partner_access_mode, article.partner_reuse_policy,
    article.created_at, article.updated_at
FROM articles AS article
WHERE article.company_id = sqlc.arg(company_id)
  AND article.status = 'published'
  AND (sqlc.narg(section_id)::uuid IS NULL
       OR article.section_id = sqlc.narg(section_id)::uuid)
  AND (
      article.partner_access_mode = 'all'
      OR (article.partner_access_mode = 'selected' AND EXISTS (
          SELECT 1 FROM article_partner_access_grants AS access_grant
          WHERE access_grant.company_id = article.company_id
            AND access_grant.article_id = article.id
            AND access_grant.partner_id = sqlc.arg(partner_id)
      ))
  )
ORDER BY article.updated_at DESC, article.id;

-- name: SearchPartnerArticles :many
SELECT article.id, article.company_id, article.section_id, article.title,
    article.content, article.status, article.author_id, article.version,
    article.requires_acknowledgement, article.plain_text,
    article.partner_access_mode, article.partner_reuse_policy,
    article.created_at, article.updated_at
FROM articles AS article
WHERE article.company_id = sqlc.arg(company_id)
  AND article.status = 'published'
  AND article.search @@ plainto_tsquery('russian', sqlc.arg(query))
  AND (
      article.partner_access_mode = 'all'
      OR (article.partner_access_mode = 'selected' AND EXISTS (
          SELECT 1 FROM article_partner_access_grants AS access_grant
          WHERE access_grant.company_id = article.company_id
            AND access_grant.article_id = article.id
            AND access_grant.partner_id = sqlc.arg(partner_id)
      ))
  )
ORDER BY ts_rank(article.search, plainto_tsquery('russian', sqlc.arg(query))) DESC,
    article.updated_at DESC, article.id;

-- name: ListPartnerVisibleSections :many
WITH RECURSIVE visible_sections AS (
    SELECT DISTINCT section.id, section.company_id, section.name,
        section.parent_id, section."order", section.access,
        section.created_at, section.updated_at, section.visibility
    FROM sections AS section
    JOIN articles AS article
      ON article.company_id = section.company_id
     AND article.section_id = section.id
    WHERE section.company_id = sqlc.arg(company_id)
      AND article.status = 'published'
      AND (
          article.partner_access_mode = 'all'
          OR (article.partner_access_mode = 'selected' AND EXISTS (
              SELECT 1 FROM article_partner_access_grants AS access_grant
              WHERE access_grant.company_id = article.company_id
                AND access_grant.article_id = article.id
                AND access_grant.partner_id = sqlc.arg(partner_id)
          ))
      )
    UNION
    SELECT parent.id, parent.company_id, parent.name, parent.parent_id,
        parent."order", parent.access, parent.created_at, parent.updated_at,
        parent.visibility
    FROM sections AS parent
    JOIN visible_sections AS child
      ON child.company_id = parent.company_id
     AND child.parent_id = parent.id
)
SELECT id, company_id, name, parent_id, "order", access,
    created_at, updated_at, visibility
FROM visible_sections
ORDER BY parent_id NULLS FIRST, "order", id;

-- name: EnsureCurrentArticleVersion :exec
INSERT INTO article_versions (
    id, company_id, article_id, version, title, content, author_id, created_at
)
SELECT sqlc.arg(version_id), article.company_id, article.id, article.version,
       article.title, article.content, article.author_id, article.updated_at
FROM articles AS article
WHERE article.company_id = sqlc.arg(company_id)
  AND article.id = sqlc.arg(article_id)
  AND article.status = 'published'
ON CONFLICT (article_id, version) DO NOTHING;

-- name: GetArticleSnapshotForCourseCopy :one
SELECT article.id AS article_id, article.company_id,
    version.id AS article_version_id, version.version AS article_version,
    version.title, version.content, article.partner_reuse_policy
FROM articles AS article
JOIN article_versions AS version
  ON version.company_id = article.company_id
 AND version.article_id = article.id
 AND version.version = article.version
WHERE article.company_id = sqlc.arg(company_id)
  AND article.id = sqlc.arg(article_id)
  AND article.status = 'published'
  AND article.partner_reuse_policy = 'copy_allowed'
  AND (
      article.partner_access_mode = 'all'
      OR (article.partner_access_mode = 'selected' AND EXISTS (
          SELECT 1 FROM article_partner_access_grants AS access_grant
          WHERE access_grant.company_id = article.company_id
            AND access_grant.article_id = article.id
            AND access_grant.partner_id = sqlc.arg(partner_id)
      ))
  );

-- name: GetArticleSnapshotReuseGrant :one
SELECT id, company_id, article_id, article_version_id, article_version,
    partner_id, requested_by_id, idempotency_key, content_hash,
    source_file_ids, granted_at
FROM article_snapshot_reuse_grants
WHERE company_id = sqlc.arg(company_id)
  AND article_id = sqlc.arg(article_id)
  AND article_version_id = sqlc.arg(article_version_id)
  AND partner_id = sqlc.arg(partner_id)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateArticleSnapshotReuseGrant :one
INSERT INTO article_snapshot_reuse_grants (
    id, company_id, article_id, article_version_id, article_version,
    partner_id, requested_by_id, idempotency_key, content_hash,
    source_file_ids, granted_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(article_id),
    sqlc.arg(article_version_id), sqlc.arg(article_version),
    sqlc.arg(partner_id), sqlc.arg(requested_by_id),
    sqlc.arg(idempotency_key), sqlc.arg(content_hash),
    sqlc.arg(source_file_ids), sqlc.arg(granted_at)
)
ON CONFLICT (
    company_id, article_id, article_version_id, partner_id, idempotency_key
) DO NOTHING
RETURNING id, company_id, article_id, article_version_id, article_version,
    partner_id, requested_by_id, idempotency_key, content_hash,
    source_file_ids, granted_at;
