-- name: CreateAuditLogEntry :one
INSERT INTO audit_log (
    id, company_id, actor_id, actor_role, action, aggregate_type,
    aggregate_id, before_state, after_state, reason, request_id, ip_hash, created_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(actor_id), sqlc.arg(actor_role),
    sqlc.arg(action), sqlc.arg(aggregate_type), sqlc.arg(aggregate_id),
    sqlc.narg(before_state), sqlc.narg(after_state), sqlc.narg(reason),
    sqlc.narg(request_id), sqlc.narg(ip_hash), sqlc.arg(created_at)
)
RETURNING id, company_id, actor_id, actor_role, action, aggregate_type,
    aggregate_id, before_state, after_state, reason, request_id, ip_hash, created_at;

-- name: GetAggregateAuditLog :many
SELECT id, company_id, actor_id, actor_role, action, aggregate_type,
    aggregate_id, before_state, after_state, reason, request_id, ip_hash, created_at
FROM audit_log
WHERE company_id = sqlc.arg(company_id)
  AND aggregate_type = sqlc.arg(aggregate_type)
  AND aggregate_id = sqlc.arg(aggregate_id)
ORDER BY created_at DESC, id DESC;
