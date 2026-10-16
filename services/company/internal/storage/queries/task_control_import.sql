-- name: GetTaskControlImportObject :one
SELECT payload FROM task_control_import_objects WHERE company_id=$1 AND binding_id=$2 AND kind=$3 AND id=$4;
-- name: SaveTaskControlImportObject :exec
INSERT INTO task_control_import_objects(company_id,binding_id,kind,id,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(company_id,binding_id,kind,id) DO UPDATE SET payload=excluded.payload;
-- name: ListTaskControlImportSources :many
SELECT payload FROM task_control_import_objects WHERE company_id=$1 AND binding_id=$2 AND kind='source' AND payload->>'state'='pending' AND (payload->>'expiresAt')::timestamptz > sqlc.arg(now)::timestamptz ORDER BY id LIMIT 100;
-- name: GetTaskControlImportSourceSnapshot :one
SELECT payload FROM task_control_import_objects WHERE company_id=$1 AND binding_id=$2 AND kind='source' AND payload->>'sourceSnapshotId'=sqlc.arg(snapshot_id)::text;
