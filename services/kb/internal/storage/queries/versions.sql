-- name: ListArticleVersions :many
SELECT id, company_id, article_id, version, title, content, author_id, created_at
FROM article_versions
WHERE company_id = $1 AND article_id = $2
ORDER BY version DESC, created_at DESC;

-- name: GetArticleVersion :one
SELECT id, company_id, article_id, version, title, content, author_id, created_at
FROM article_versions
WHERE company_id = $1 AND id = $2;

-- name: CreateArticleVersion :one
INSERT INTO article_versions (id, company_id, article_id, version, title, content, author_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, company_id, article_id, version, title, content, author_id, created_at;