-- name: ListAcknowledgements :many
SELECT article_id, user_id, acknowledged_at
FROM acknowledgements
WHERE company_id = $1 AND article_id = $2
ORDER BY acknowledged_at DESC;

-- name: UpsertAcknowledgement :exec
INSERT INTO acknowledgements (company_id, article_id, user_id, acknowledged_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (article_id, user_id) DO UPDATE
SET acknowledged_at = EXCLUDED.acknowledged_at;