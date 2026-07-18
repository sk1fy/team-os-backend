-- name: ListSections :many
SELECT id, company_id, name, parent_id, "order", access, created_at, updated_at, visibility
FROM sections
WHERE company_id = $1
ORDER BY parent_id NULLS FIRST, "order", name;

-- name: GetSection :one
SELECT id, company_id, name, parent_id, "order", access, created_at, updated_at, visibility
FROM sections
WHERE company_id = $1 AND id = $2;

-- name: CountChildSections :one
SELECT count(*)::bigint AS count
FROM sections
WHERE company_id = $1 AND parent_id = $2;

-- name: CountSectionArticles :one
SELECT count(*)::bigint AS count
FROM articles
WHERE company_id = $1 AND section_id = $2;

-- name: CountSectionSiblings :one
SELECT count(*)::int AS count
FROM sections
WHERE company_id = $1 AND parent_id IS NOT DISTINCT FROM $2;

-- name: CreateSection :one
INSERT INTO sections (id, company_id, name, parent_id, "order", access, visibility)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, company_id, name, parent_id, "order", access, created_at, updated_at, visibility;

-- name: UpdateSection :one
UPDATE sections
SET
    name = COALESCE(sqlc.narg(name), name),
    access = COALESCE(sqlc.narg(access), access),
    visibility = COALESCE(sqlc.narg(visibility), visibility),
    updated_at = now()
WHERE company_id = $1 AND id = $2
RETURNING id, company_id, name, parent_id, "order", access, created_at, updated_at, visibility;

-- name: DeleteSection :exec
DELETE FROM sections
WHERE company_id = $1 AND id = $2;
