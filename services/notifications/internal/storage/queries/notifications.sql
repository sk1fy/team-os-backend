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

-- name: UpsertNotificationUser :exec
INSERT INTO notification_users (company_id, user_id, active, position_ids, department_ids, last_event_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (company_id, user_id) DO UPDATE
SET active = EXCLUDED.active,
    position_ids = EXCLUDED.position_ids,
    department_ids = EXCLUDED.department_ids,
    last_event_at = EXCLUDED.last_event_at
WHERE notification_users.last_event_at <= EXCLUDED.last_event_at;

-- name: DeactivateNotificationUser :exec
INSERT INTO notification_users (company_id, user_id, active, position_ids, department_ids, last_event_at)
VALUES ($1, $2, false, '{}', '{}', $3)
ON CONFLICT (company_id, user_id) DO UPDATE
SET active = false,
    last_event_at = EXCLUDED.last_event_at
WHERE notification_users.last_event_at <= EXCLUDED.last_event_at;

-- name: ResolveArticleAudience :many
SELECT user_id
FROM notification_users
WHERE company_id = sqlc.arg(company_id)
  AND active
  AND (
    sqlc.arg(company_wide)::boolean
    OR user_id = ANY(sqlc.arg(user_ids)::uuid[])
    OR position_ids && sqlc.arg(position_ids)::uuid[]
    OR department_ids && sqlc.arg(department_ids)::uuid[]
  )
ORDER BY user_id;
