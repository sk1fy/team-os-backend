-- name: SaveTaskControlRuntimeIssued :exec
INSERT INTO task_control_runtime_issued(company_id,binding_id,mode_epoch,actor_id,issue_key,publication_id,publication_version,runtime_revision,payload_checksum,issued_at,valid_until)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT(company_id,binding_id,mode_epoch,actor_id,issue_key) DO UPDATE SET issued_at=excluded.issued_at,valid_until=excluded.valid_until WHERE task_control_runtime_issued.payload_checksum=excluded.payload_checksum;
-- name: GetTaskControlRuntimeIssued :one
SELECT * FROM task_control_runtime_issued WHERE company_id=$1 AND binding_id=$2 AND mode_epoch=$3 AND actor_id=$4 AND issue_key=$5;
-- name: GetTaskControlRuntimeEvent :one
SELECT * FROM task_control_runtime_events WHERE company_id=$1 AND binding_id=$2 AND event_id=$3;
-- name: SaveTaskControlRuntimeEvent :exec
INSERT INTO task_control_runtime_events(company_id,binding_id,event_id,mode_epoch,actor_id,runtime_instance_id,publication_id,runtime_revision,event,payload_hash,payload,received_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);
-- name: CountTaskControlRuntimeEvents :one
SELECT count(*) FROM task_control_runtime_events WHERE company_id=$1 AND binding_id=$2 AND received_at>=sqlc.arg(since)::timestamptz AND (runtime_instance_id=sqlc.arg(runtime_instance_id)::uuid OR sqlc.arg(runtime_instance_id)::uuid='00000000-0000-0000-0000-000000000000');
-- name: ListTaskControlRuntimeEvents :many
SELECT * FROM task_control_runtime_events WHERE company_id=$1 AND binding_id=$2 AND received_at>=sqlc.arg(since)::timestamptz ORDER BY received_at DESC,sequence DESC LIMIT 2000;
-- name: HasTaskControlRuntimeApplied :one
SELECT EXISTS(SELECT 1 FROM task_control_runtime_events WHERE company_id=$1 AND binding_id=$2 AND mode_epoch=$3 AND actor_id=$4 AND runtime_instance_id=$5 AND publication_id=$6 AND runtime_revision=$7 AND event='applied' AND received_at>=sqlc.arg(since)::timestamptz);
-- name: PruneTaskControlRuntimeEvents :exec
DELETE FROM task_control_runtime_events WHERE (company_id,binding_id,event_id) IN (SELECT company_id,binding_id,event_id FROM task_control_runtime_events WHERE received_at<sqlc.arg(before)::timestamptz ORDER BY received_at LIMIT 1000);
-- name: PruneTaskControlRuntimeIssued :exec
DELETE FROM task_control_runtime_issued WHERE (company_id,binding_id,mode_epoch,actor_id,issue_key) IN (SELECT company_id,binding_id,mode_epoch,actor_id,issue_key FROM task_control_runtime_issued WHERE valid_until<sqlc.arg(before)::timestamptz ORDER BY valid_until LIMIT 1000);
-- name: GetTaskControlRuntimeInstanceActor :one
SELECT actor_id FROM task_control_runtime_events WHERE company_id=$1 AND binding_id=$2 AND runtime_instance_id=$3 ORDER BY received_at DESC,sequence DESC LIMIT 1;
-- name: CountTaskControlRuntimeInstances :one
SELECT count(DISTINCT runtime_instance_id) FROM task_control_runtime_events WHERE company_id=$1 AND binding_id=$2 AND received_at>=sqlc.arg(since)::timestamptz;
