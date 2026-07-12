-- name: ListSchedules :many
SELECT * FROM user_schedules WHERE company_id = $1 ORDER BY user_id;

-- name: UpsertSchedule :one
INSERT INTO user_schedules (company_id, user_id, template)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE
SET template = EXCLUDED.template, updated_at = now()
WHERE user_schedules.company_id = EXCLUDED.company_id
RETURNING *;

-- name: ListShiftExceptionsByMonth :many
SELECT * FROM shift_exceptions
WHERE company_id = $1 AND date >= $2 AND date < $3
ORDER BY date, user_id;

-- name: UpsertShiftException :one
INSERT INTO shift_exceptions (
    id, company_id, user_id, date, type, start_time, end_time, note
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (user_id, date) DO UPDATE
SET type = EXCLUDED.type,
    start_time = EXCLUDED.start_time,
    end_time = EXCLUDED.end_time,
    note = EXCLUDED.note,
    updated_at = now()
WHERE shift_exceptions.company_id = EXCLUDED.company_id
RETURNING *;
