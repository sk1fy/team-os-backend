-- name: ListArticles :many
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, created_at, updated_at
FROM articles
WHERE company_id = $1
  AND (sqlc.narg(section_id)::uuid IS NULL OR section_id = sqlc.narg(section_id))
ORDER BY updated_at DESC;

-- name: GetArticle :one
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, created_at, updated_at
FROM articles
WHERE company_id = $1 AND id = $2;

-- name: GetArticleForUpdate :one
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, created_at, updated_at
FROM articles
WHERE company_id = $1 AND id = $2
FOR UPDATE;

-- name: GetArticlesByIDs :many
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, created_at, updated_at
FROM articles
WHERE company_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY updated_at DESC;

-- name: ArticleExists :one
SELECT EXISTS (
    SELECT 1 FROM articles WHERE company_id = $1 AND id = $2
) AS exists;

-- name: SearchArticles :many
SELECT id, company_id, section_id, title, content, status, author_id, version,
       requires_acknowledgement, plain_text, created_at, updated_at
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
          requires_acknowledgement, plain_text, created_at, updated_at;

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
          requires_acknowledgement, plain_text, created_at, updated_at;
