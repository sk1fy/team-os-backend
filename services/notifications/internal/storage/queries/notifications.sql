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

-- name: InsertEmailDelivery :exec
INSERT INTO email_deliveries (
  id, event_id, company_id, challenge_id, purpose, recipient_fingerprint,
  expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT DO NOTHING;

-- name: ExpireEmailDelivery :exec
UPDATE email_deliveries
SET status = 'expired', updated_at = sqlc.arg(now_at)
WHERE company_id = sqlc.arg(company_id)
  AND challenge_id = sqlc.arg(challenge_id)
  AND status <> 'sent'
  AND expires_at <= sqlc.arg(now_at);

-- name: ClaimEmailDelivery :one
UPDATE email_deliveries
SET status = 'sending',
    attempts = attempts + 1,
    last_attempt_at = sqlc.arg(now_at),
    last_error_code = NULL,
    updated_at = sqlc.arg(now_at)
WHERE company_id = sqlc.arg(company_id)
  AND challenge_id = sqlc.arg(challenge_id)
  AND expires_at > sqlc.arg(now_at)
  AND attempts < max_attempts
  AND (
    status IN ('pending', 'failed')
    OR (status = 'sending' AND last_attempt_at <= sqlc.arg(stale_before))
  )
RETURNING id, event_id, company_id, challenge_id, purpose,
  recipient_fingerprint, status, attempts, max_attempts, expires_at,
  last_attempt_at, sent_at, last_error_code, created_at, updated_at;

-- name: GetEmailDelivery :one
SELECT id, event_id, company_id, challenge_id, purpose,
  recipient_fingerprint, status, attempts, max_attempts, expires_at,
  last_attempt_at, sent_at, last_error_code, created_at, updated_at
FROM email_deliveries
WHERE company_id = $1 AND challenge_id = $2;

-- name: MarkEmailDeliverySent :exec
UPDATE email_deliveries
SET status = 'sent', sent_at = sqlc.arg(sent_at), last_error_code = NULL,
    updated_at = sqlc.arg(sent_at)
WHERE company_id = sqlc.arg(company_id)
  AND challenge_id = sqlc.arg(challenge_id)
  AND status = 'sending';

-- name: MarkEmailDeliveryFailed :exec
UPDATE email_deliveries
SET status = 'failed', last_error_code = sqlc.arg(error_code),
    updated_at = sqlc.arg(failed_at)
WHERE company_id = sqlc.arg(company_id)
  AND challenge_id = sqlc.arg(challenge_id)
  AND status = 'sending';
