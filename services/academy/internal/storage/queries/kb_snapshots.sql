-- name: GetKBArticleSnapshot :one
SELECT id, company_id, source_article_id, source_article_version_id,
    source_article_version_number, reuse_grant_id, requested_by_id,
    requested_by_partner_id, request_key, title, content, source_file_ids,
    content_hash, created_at
FROM kb_article_snapshots
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id);

-- name: GetKBArticleSnapshotByRequestKey :one
SELECT id, company_id, source_article_id, source_article_version_id,
    source_article_version_number, reuse_grant_id, requested_by_id,
    requested_by_partner_id, request_key, title, content, source_file_ids,
    content_hash, created_at
FROM kb_article_snapshots
WHERE company_id = sqlc.arg(company_id)
  AND request_key = sqlc.arg(request_key);

-- name: CreateKBArticleSnapshot :one
INSERT INTO kb_article_snapshots (
    id, company_id, source_article_id, source_article_version_id,
    source_article_version_number, reuse_grant_id, requested_by_id,
    requested_by_partner_id, request_key, title, content, source_file_ids,
    content_hash, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(source_article_id),
    sqlc.narg(source_article_version_id),
    sqlc.narg(source_article_version_number), sqlc.narg(reuse_grant_id),
    sqlc.arg(requested_by_id), sqlc.narg(requested_by_partner_id),
    sqlc.arg(request_key), sqlc.arg(title), sqlc.arg(content),
    sqlc.arg(source_file_ids), sqlc.arg(content_hash), sqlc.arg(created_at)
)
ON CONFLICT (company_id, request_key) DO NOTHING
RETURNING id, company_id, source_article_id, source_article_version_id,
    source_article_version_number, reuse_grant_id, requested_by_id,
    requested_by_partner_id, request_key, title, content, source_file_ids,
    content_hash, created_at;

-- name: ListKBArticleSnapshotsBySource :many
SELECT id, company_id, source_article_id, source_article_version_id,
    source_article_version_number, reuse_grant_id, requested_by_id,
    requested_by_partner_id, request_key, title, content, source_file_ids,
    content_hash, created_at
FROM kb_article_snapshots
WHERE company_id = sqlc.arg(company_id)
  AND source_article_id = sqlc.arg(source_article_id)
ORDER BY created_at DESC, id;
