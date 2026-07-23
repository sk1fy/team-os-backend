-- name: ListArticles :many
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, partner_access_mode,
       partner_reuse_policy, created_at, updated_at
FROM articles
WHERE company_id = $1
  AND (sqlc.narg(section_id)::uuid IS NULL OR section_id = sqlc.narg(section_id))
ORDER BY updated_at DESC;

-- name: GetArticle :one
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, partner_access_mode,
       partner_reuse_policy, created_at, updated_at
FROM articles
WHERE company_id = $1 AND id = $2;

-- name: GetPublicArticle :one
SELECT a.id, a.company_id, a.section_id, a.title, a.content, a.status, a.author_id, a.version,
       a.requires_acknowledgement, a.plain_text, a.partner_access_mode,
       a.partner_reuse_policy, a.created_at, a.updated_at
FROM articles a
JOIN sections s ON s.id = a.section_id AND s.company_id = a.company_id
WHERE a.id = $1 AND a.status = 'published' AND s.visibility = 'public';

-- name: GetArticleForUpdate :one
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, partner_access_mode,
       partner_reuse_policy, created_at, updated_at
FROM articles
WHERE company_id = $1 AND id = $2
FOR UPDATE;

-- name: GetArticlesByIDs :many
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, partner_access_mode,
       partner_reuse_policy, created_at, updated_at
FROM articles
WHERE company_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY updated_at DESC;

-- name: ArticleExists :one
SELECT EXISTS (
    SELECT 1 FROM articles WHERE company_id = $1 AND id = $2
) AS exists;

-- name: SearchArticles :many
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, partner_access_mode,
       partner_reuse_policy, created_at, updated_at
FROM articles
WHERE company_id = $1
  AND search @@ plainto_tsquery('russian', sqlc.arg(query))
ORDER BY ts_rank(search, plainto_tsquery('russian', sqlc.arg(query))) DESC, updated_at DESC;

-- name: CreateArticle :one
INSERT INTO articles (
    id, company_id, section_id, title, content, status, author_id, version,
    requires_acknowledgement, plain_text
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, company_id, section_id, title, content, status, author_id, version,
          requires_acknowledgement, plain_text, partner_access_mode,
          partner_reuse_policy, created_at, updated_at;

-- name: UpdateArticle :one
UPDATE articles
SET
    section_id = COALESCE(sqlc.narg(section_id), section_id),
    title = COALESCE(sqlc.narg(title), title),
    content = COALESCE(sqlc.narg(content), content),
    status = COALESCE(sqlc.narg(status), status),
    requires_acknowledgement = COALESCE(sqlc.narg(requires_acknowledgement), requires_acknowledgement),
    plain_text = COALESCE(sqlc.narg(plain_text), plain_text),
    version = COALESCE(sqlc.narg(version), version),
    updated_at = now()
WHERE company_id = $1 AND id = $2
RETURNING id, company_id, section_id, title, content, status, author_id, version,
          requires_acknowledgement, plain_text, partner_access_mode,
          partner_reuse_policy, created_at, updated_at;
