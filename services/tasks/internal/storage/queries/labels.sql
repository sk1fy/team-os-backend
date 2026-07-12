-- name: ListLabels :many
SELECT id, company_id, name, color
FROM labels
WHERE company_id = $1
ORDER BY name;