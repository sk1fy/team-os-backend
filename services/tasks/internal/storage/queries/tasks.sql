-- name: ListTasks :many
SELECT
    id, company_id, board_id, column_id, "order", title, description, author_id,
    assignee_ids, assignee_position_id, watcher_ids, due_date, priority, label_ids,
    checklist, attachments, source, linked_article_ids, recurrence, recurrence_generated_at, completed_at,
    due_soon_sent_at, created_at, updated_at
FROM tasks
WHERE company_id = $1
  AND (sqlc.narg(board_id)::uuid IS NULL OR board_id = sqlc.narg(board_id))
ORDER BY board_id, column_id, "order";

-- name: GetTask :one
SELECT
    id, company_id, board_id, column_id, "order", title, description, author_id,
    assignee_ids, assignee_position_id, watcher_ids, due_date, priority, label_ids,
    checklist, attachments, source, linked_article_ids, recurrence, recurrence_generated_at, completed_at,
    due_soon_sent_at, created_at, updated_at
FROM tasks
WHERE company_id = $1 AND id = $2;

-- name: GetTaskForUpdate :one
SELECT
    id, company_id, board_id, column_id, "order", title, description, author_id,
    assignee_ids, assignee_position_id, watcher_ids, due_date, priority, label_ids,
    checklist, attachments, source, linked_article_ids, recurrence, recurrence_generated_at, completed_at,
    due_soon_sent_at, created_at, updated_at
FROM tasks
WHERE company_id = $1 AND id = $2
FOR UPDATE;

-- name: LockBoardOrder :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(board_id)::uuid::text, 0));

-- name: ListTasksInColumnsForUpdate :many
SELECT id, column_id, "order"
FROM tasks
WHERE company_id = $1 AND column_id = ANY(sqlc.arg(column_ids)::uuid[])
FOR UPDATE;

-- name: CountTasksInColumn :one
SELECT count(*)::integer AS count
FROM tasks
WHERE column_id = $1;

-- name: IsRecurrenceGenerated :one
SELECT (recurrence_generated_at IS NOT NULL)::boolean AS generated
FROM tasks
WHERE company_id = $1 AND id = $2;

-- name: MarkRecurrenceGenerated :exec
UPDATE tasks
SET recurrence_generated_at = now()
WHERE company_id = $1 AND id = $2;

-- name: CreateTask :one
INSERT INTO tasks (
    id, company_id, board_id, column_id, "order", title, description, author_id,
    assignee_ids, assignee_position_id, watcher_ids, due_date, priority, label_ids,
    checklist, attachments, source, linked_article_ids, recurrence, completed_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
)
RETURNING
    id, company_id, board_id, column_id, "order", title, description, author_id,
    assignee_ids, assignee_position_id, watcher_ids, due_date, priority, label_ids,
    checklist, attachments, source, linked_article_ids, recurrence, recurrence_generated_at, completed_at,
    due_soon_sent_at, created_at, updated_at;

-- name: UpdateTask :one
UPDATE tasks
SET
    title = COALESCE(sqlc.narg(title), title),
    description = COALESCE(sqlc.narg(description), description),
    assignee_ids = COALESCE(sqlc.narg(assignee_ids), assignee_ids),
    assignee_position_id = CASE
        WHEN sqlc.narg(clear_assignee_position_id)::boolean THEN NULL
        ELSE COALESCE(sqlc.narg(assignee_position_id), assignee_position_id)
    END,
    watcher_ids = COALESCE(sqlc.narg(watcher_ids), watcher_ids),
    due_date = CASE
        WHEN sqlc.narg(clear_due_date)::boolean THEN NULL
        ELSE COALESCE(sqlc.narg(due_date), due_date)
    END,
    due_soon_sent_at = CASE
        WHEN sqlc.narg(clear_due_date)::boolean THEN NULL
        WHEN sqlc.narg(due_date) IS NOT NULL AND due_date IS DISTINCT FROM sqlc.narg(due_date) THEN NULL
        ELSE due_soon_sent_at
    END,
    priority = COALESCE(sqlc.narg(priority), priority),
    label_ids = COALESCE(sqlc.narg(label_ids), label_ids),
    checklist = COALESCE(sqlc.narg(checklist), checklist),
    attachments = COALESCE(sqlc.narg(attachments), attachments),
    source = COALESCE(sqlc.narg(source), source),
    linked_article_ids = COALESCE(sqlc.narg(linked_article_ids), linked_article_ids),
    recurrence = CASE
        WHEN sqlc.narg(clear_recurrence)::boolean THEN NULL
        ELSE COALESCE(sqlc.narg(recurrence), recurrence)
    END,
    completed_at = CASE
        WHEN sqlc.narg(clear_completed_at)::boolean THEN NULL
        ELSE COALESCE(sqlc.narg(completed_at), completed_at)
    END,
    updated_at = now()
WHERE company_id = $1 AND id = $2
RETURNING
    id, company_id, board_id, column_id, "order", title, description, author_id,
    assignee_ids, assignee_position_id, watcher_ids, due_date, priority, label_ids,
    checklist, attachments, source, linked_article_ids, recurrence, recurrence_generated_at, completed_at,
    due_soon_sent_at, created_at, updated_at;

-- name: UpdateTaskPosition :exec
UPDATE tasks
SET column_id = $3, "order" = $4, updated_at = now()
WHERE company_id = $1 AND id = $2;

-- name: UpdateTaskOrder :exec
UPDATE tasks
SET "order" = $3, updated_at = now()
WHERE company_id = $1 AND id = $2;

-- name: ListTasksDueSoon :many
SELECT
    id, company_id, board_id, column_id, "order", title, description, author_id,
    assignee_ids, assignee_position_id, watcher_ids, due_date, priority, label_ids,
    checklist, attachments, source, linked_article_ids, recurrence, recurrence_generated_at, completed_at,
    due_soon_sent_at, created_at, updated_at
FROM tasks
WHERE company_id = $1
  AND completed_at IS NULL
  AND due_date IS NOT NULL
  AND due_soon_sent_at IS NULL
  AND due_date > now()
  AND due_date <= now() + interval '24 hours'
ORDER BY due_date, id
FOR UPDATE SKIP LOCKED
LIMIT 100;

-- name: MarkDueSoonSent :exec
UPDATE tasks
SET due_soon_sent_at = now()
WHERE company_id = $1 AND id = $2;
