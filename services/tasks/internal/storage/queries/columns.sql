-- name: ListColumnsByBoard :many
SELECT id, board_id, name, color, "order"
FROM columns
WHERE board_id = $1
ORDER BY "order";

-- name: GetColumn :one
SELECT c.id, c.board_id, c.name, c.color, c."order", b.company_id
FROM columns c
JOIN boards b ON b.id = c.board_id
WHERE b.company_id = $1 AND c.id = $2;

-- name: CountColumnsByBoard :one
SELECT count(*)::integer AS count
FROM columns
WHERE board_id = $1;

-- name: CreateColumn :one
INSERT INTO columns (id, board_id, name, color, "order")
VALUES ($1, $2, $3, $4, $5)
RETURNING id, board_id, name, color, "order";

-- name: UpdateColumn :one
UPDATE columns
SET
    name = COALESCE(sqlc.narg(name), name),
    color = COALESCE(sqlc.narg(color), color)
WHERE id = $1
RETURNING id, board_id, name, color, "order";