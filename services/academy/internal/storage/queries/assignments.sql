-- name: GetAssignments :many
SELECT id, company_id, course_id, assignee_type, assignee_id, invite_token,
    due_date, resolved_user_ids, due_soon_sent_at, assigned_by_id, created_at
FROM assignments
WHERE company_id = $1
ORDER BY created_at, id;

-- name: GetUserAssignments :many
SELECT id, company_id, course_id, assignee_type, assignee_id, invite_token,
    due_date, resolved_user_ids, due_soon_sent_at, assigned_by_id, created_at
FROM assignments
WHERE company_id = $1 AND sqlc.arg(user_id)::uuid = ANY(resolved_user_ids)
ORDER BY created_at, id;

-- name: RecomputeUserAssignmentMembership :exec
UPDATE assignments
SET resolved_user_ids = ARRAY(
    SELECT DISTINCT candidate
    FROM unnest(
        array_remove(resolved_user_ids, sqlc.arg(user_id)::uuid) ||
        CASE
            WHEN sqlc.arg(active)::boolean AND (
                (assignee_type = 'user' AND assignee_id = sqlc.arg(user_id)::uuid) OR
                (assignee_type = 'position' AND assignee_id = ANY(sqlc.arg(position_ids)::uuid[])) OR
                (assignee_type = 'department' AND assignee_id = ANY(sqlc.arg(department_ids)::uuid[]))
            ) THEN ARRAY[sqlc.arg(user_id)::uuid]
            ELSE '{}'::uuid[]
        END
    ) AS candidate
)
WHERE company_id = sqlc.arg(company_id)
  AND assignee_type IN ('user', 'position', 'department');

-- name: ClearDeletedPositionAssignment :exec
UPDATE assignments
SET resolved_user_ids = '{}'
WHERE company_id = $1 AND assignee_type = 'position' AND assignee_id = $2;

-- name: CreateAssignment :one
INSERT INTO assignments (
    id, company_id, course_id, assignee_type, assignee_id, invite_token,
    due_date, resolved_user_ids, assigned_by_id, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT DO NOTHING
RETURNING id, company_id, course_id, assignee_type, assignee_id, invite_token,
    due_date, resolved_user_ids, due_soon_sent_at, assigned_by_id, created_at;

-- name: GetAssignmentByTarget :one
SELECT id, company_id, course_id, assignee_type, assignee_id, invite_token,
    due_date, resolved_user_ids, due_soon_sent_at, assigned_by_id, created_at
FROM assignments
WHERE company_id = $1
  AND course_id = $2
  AND assignee_type = $3
  AND assignee_id IS NOT DISTINCT FROM sqlc.narg(assignee_id)::uuid;

-- name: GetDueSoonAssignments :many
SELECT a.id, a.company_id, a.course_id, a.assignee_type, a.assignee_id,
    a.invite_token, a.resolved_user_ids, a.assigned_by_id, a.created_at,
    c.title AS course_title,
    coalesce(a.due_date, a.created_at + make_interval(days => c.deadline_days)) AS effective_due_date
FROM assignments a
JOIN courses c ON c.id = a.course_id
WHERE a.due_soon_sent_at IS NULL
  AND (a.due_date IS NOT NULL OR c.deadline_days IS NOT NULL)
  AND coalesce(a.due_date, a.created_at + make_interval(days => c.deadline_days))
      <= sqlc.arg(threshold)::timestamptz
ORDER BY a.created_at, a.id
FOR UPDATE OF a SKIP LOCKED;

-- name: MarkAssignmentDueSoonSent :exec
UPDATE assignments
SET due_soon_sent_at = $2
WHERE id = $1;

-- name: GetOverdueAssignments :many
SELECT a.id, a.company_id, a.course_id, a.resolved_user_ids,
    coalesce(a.due_date, a.created_at + make_interval(days => c.deadline_days)) AS effective_due_date
FROM assignments a
JOIN courses c ON c.id = a.course_id
WHERE (a.due_date IS NOT NULL OR c.deadline_days IS NOT NULL)
  AND coalesce(a.due_date, a.created_at + make_interval(days => c.deadline_days))
      < sqlc.arg(now)::timestamptz
ORDER BY a.created_at, a.id;
