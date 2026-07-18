-- name: ListNotifications :many
SELECT id, user_id, type, title, body, link, read, created_at
FROM notifications
WHERE company_id = $1 AND user_id = $2
ORDER BY created_at DESC;

-- name: CountUnread :one
SELECT count(*)
FROM notifications
WHERE company_id = $1 AND user_id = $2 AND read = false;

-- name: MarkRead :execrows
UPDATE notifications
SET read = true
WHERE id = $1 AND company_id = $2 AND user_id = $3;

-- name: MarkAllRead :exec
UPDATE notifications
SET read = true
WHERE company_id = $1 AND user_id = $2 AND read = false;

-- name: InsertProcessedEvent :execrows
INSERT INTO processed_events (event_id, company_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: InsertNotification :exec
INSERT INTO notifications (id, company_id, user_id, type, title, body, link)
VALUES ($1, $2, $3, $4, $5, $6, $7);
