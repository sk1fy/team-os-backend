-- name: SetTaskControlAuditActor :exec
SELECT set_config('task_control.actor_id',sqlc.arg(actor_id)::text,true);
-- name: GetTaskControlAuditHighWater :one
SELECT coalesce(max(sequence),0)::bigint FROM task_control_audit_events WHERE company_id=$1 AND binding_id=$2;
-- name: ListTaskControlAuditEvents :many
SELECT * FROM task_control_audit_events WHERE company_id=$1 AND binding_id=$2 AND sequence<=sqlc.arg(high_water)::bigint AND sequence<sqlc.arg(before_sequence)::bigint ORDER BY sequence DESC LIMIT sqlc.arg(page_limit)::integer;
-- name: ListTaskControlPublicationsPage :many
SELECT * FROM task_control_publications WHERE company_id=$1 AND binding_id=$2 AND payload->>'schemaVersion'=sqlc.arg(schema_version)::text AND version<=sqlc.arg(high_water)::bigint AND version<sqlc.arg(before_version)::bigint ORDER BY version DESC LIMIT sqlc.arg(page_limit)::integer;

-- name: GetTaskControlPublicationHighWater :one
SELECT coalesce(max(version),0)::bigint FROM task_control_publications WHERE company_id=$1 AND binding_id=$2;
