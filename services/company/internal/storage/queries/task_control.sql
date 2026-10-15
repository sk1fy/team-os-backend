-- name: LockTaskControlAccount :exec
SELECT pg_advisory_xact_lock(hashtextextended('task-control:' || sqlc.arg(account_id)::text,0));
-- name: GetTaskControlPermissions :one
SELECT * FROM task_control_permissions WHERE company_id=$1 AND user_id=$2;
-- name: GetTaskControlSnapshot :one
SELECT * FROM task_control_installation_snapshots WHERE company_id=$1 AND account_id=$2;
-- name: SaveTaskControlSnapshot :exec
INSERT INTO task_control_installation_snapshots(company_id,account_id,payload,checked_at,fresh_until) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(company_id,account_id) DO UPDATE SET payload=excluded.payload,checked_at=excluded.checked_at,fresh_until=excluded.fresh_until
WHERE task_control_installation_snapshots.checked_at<=excluded.checked_at
AND ((task_control_installation_snapshots.payload->>'installationId') IS DISTINCT FROM (excluded.payload->>'installationId')
 OR coalesce((task_control_installation_snapshots.payload->>'revision')::bigint,0)<=coalesce((excluded.payload->>'revision')::bigint,0));
-- name: ListTaskControlConnections :many
SELECT * FROM task_control_connections WHERE company_id=$1 ORDER BY updated_at DESC,id;
-- name: GetTaskControlConnection :one
SELECT * FROM task_control_connections WHERE company_id=$1 AND id=$2;
-- name: GetTaskControlIntent :one
SELECT * FROM task_control_connections WHERE company_id=$1 AND intent_id=$2;
-- name: ActiveTaskControlConnection :one
SELECT * FROM task_control_connections WHERE company_id=$1 AND account_id=$2 AND state='active';
-- name: CreateTaskControlConnection :one
INSERT INTO task_control_connections(company_id,id,installation_id,integration_id,account_id,generation,intent_id,linked_by,crm_admin_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;
-- name: RevokeTaskControlConnection :one
UPDATE task_control_connections SET state='revoked',binding_revision=binding_revision+1,mode_epoch=mode_epoch+1,updated_at=now()
WHERE company_id=$1 AND id=$2 AND binding_revision=$3 AND mode_epoch=$4 AND state='active' RETURNING *;
-- name: ConsumeTaskControlAssertion :execrows
INSERT INTO task_control_assertion_uses(key_id,jti,company_id,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;
-- name: CreateTaskControlRuntimeSession :exec
INSERT INTO task_control_runtime_sessions(token_hash,company_id,binding_id,actor_id,binding_revision,mode_epoch,generation,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: GetTaskControlRuntimeSession :one
SELECT * FROM task_control_runtime_sessions WHERE token_hash=$1 AND expires_at>$2;
-- name: DeleteTaskControlRuntimeSessions :exec
DELETE FROM task_control_runtime_sessions WHERE company_id=$1 AND binding_id=$2;
-- name: GetTaskControlConnectionRequest :one
SELECT * FROM task_control_connection_requests WHERE company_id=$1 AND request_id=$2;
-- name: SaveTaskControlConnectionRequest :exec
INSERT INTO task_control_connection_requests(company_id,request_id,binding_id,actor_id,action,body_hash,response) VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: EnsureTaskControlDraft :exec
INSERT INTO task_control_drafts(company_id,binding_id,checksum,updated_by) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;
-- name: GetTaskControlDraft :one
SELECT * FROM task_control_drafts WHERE company_id=$1 AND binding_id=$2;
-- name: UpdateTaskControlDraft :one
UPDATE task_control_drafts SET rules=$3,checksum=$4,revision=revision+1,updated_at=now(),updated_by=$5 WHERE company_id=$1 AND binding_id=$2 AND revision=$6 RETURNING *;
-- name: GetTaskControlPublication :one
SELECT * FROM task_control_publications WHERE company_id=$1 AND binding_id=$2 AND id=$3;
-- name: GetTaskControlPublishRequest :one
SELECT * FROM task_control_publications WHERE company_id=$1 AND request_id=$2;
-- name: ListTaskControlPublications :many
SELECT * FROM task_control_publications WHERE company_id=$1 AND binding_id=$2 ORDER BY version DESC LIMIT 100;
-- name: CreateTaskControlPublication :one
INSERT INTO task_control_publications(company_id,binding_id,id,version,request_id,body_hash,payload)
VALUES($1,$2,$3,(SELECT coalesce(max(version),0)+1 FROM task_control_publications WHERE company_id=$1 AND binding_id=$2),$4,$5,$6) RETURNING *;
-- name: SetTaskControlPublication :one
UPDATE task_control_connections SET publication_id=$3,updated_at=now() WHERE company_id=$1 AND id=$2 AND state='active' RETURNING *;
-- name: SetTaskControlMode :one
UPDATE task_control_connections SET mode=$3,mode_epoch=mode_epoch+1,updated_at=now() WHERE company_id=$1 AND id=$2 AND binding_revision=$4 AND mode_epoch=$5 AND state='active' RETURNING *;
