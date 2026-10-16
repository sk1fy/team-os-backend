-- name: UpdateTaskControlDraftV2 :one
UPDATE task_control_drafts SET rules=$3,checksum=$4,revision=revision+1,updated_by=$5,updated_at=now(),schema_version=2 WHERE company_id=$1 AND binding_id=$2 AND revision=$6 RETURNING *;
-- name: ActivateTaskControlV2 :one
UPDATE task_control_connections SET runtime_schema_version=2,mode_epoch=mode_epoch+1,updated_at=now() WHERE company_id=$1 AND id=$2 AND runtime_schema_version=1 RETURNING *;
-- name: GetTaskControlV2State :one
SELECT * FROM task_control_v2_state WHERE company_id=$1 AND binding_id=$2;
-- name: SetTaskControlV2State :exec
INSERT INTO task_control_v2_state(company_id,binding_id,current_snapshot_id,runtime_revision,verified_source_fresh_until) VALUES($1,$2,$3,$4,$5) ON CONFLICT(company_id,binding_id) DO UPDATE SET current_snapshot_id=excluded.current_snapshot_id,runtime_revision=excluded.runtime_revision,verified_source_fresh_until=excluded.verified_source_fresh_until;
-- name: SaveTaskControlV2Snapshot :exec
INSERT INTO task_control_v2_snapshots(company_id,binding_id,id,mode_epoch,runtime_revision,publication_id,payload) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: GetTaskControlV2Snapshot :one
SELECT * FROM task_control_v2_snapshots WHERE company_id=$1 AND binding_id=$2 AND id=$3;
-- name: SaveTaskControlV2Issued :exec
INSERT INTO task_control_v2_issued(company_id,binding_id,mode_epoch,actor_id,snapshot_id,payload_checksum,payload,issued_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(company_id,binding_id,mode_epoch,actor_id,snapshot_id) DO UPDATE SET issued_at=excluded.issued_at;
-- name: GetTaskControlV2Issued :one
SELECT * FROM task_control_v2_issued WHERE company_id=$1 AND binding_id=$2 AND mode_epoch=$3 AND actor_id=$4 AND snapshot_id=$5;
-- name: GetTaskControlV2Ack :one
SELECT * FROM task_control_v2_acks WHERE company_id=$1 AND binding_id=$2 AND mode_epoch=$3 AND actor_id=$4 AND runtime_instance_id=$5 AND runtime_revision=$6;
-- name: SaveTaskControlV2Ack :exec
INSERT INTO task_control_v2_acks(company_id,binding_id,mode_epoch,actor_id,runtime_instance_id,runtime_revision,payload_checksum,payload,received_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(company_id,binding_id,mode_epoch,actor_id,runtime_instance_id,runtime_revision) DO UPDATE SET received_at=excluded.received_at,payload=excluded.payload WHERE task_control_v2_acks.payload_checksum=excluded.payload_checksum;
-- name: LatestTaskControlV2Ack :one
SELECT * FROM task_control_v2_acks WHERE company_id=$1 AND binding_id=$2 AND payload->>'publicationId'=sqlc.arg(publication_id)::text ORDER BY received_at DESC LIMIT 1;
-- name: QueueTaskControlV2Jobs :exec
INSERT INTO task_control_v2_jobs(company_id,binding_id,mode_epoch,publication_id)
SELECT c.company_id,c.id,c.mode_epoch,c.publication_id FROM task_control_connections c JOIN companies co ON co.id=c.company_id AND co.status='active' WHERE c.state='active' AND c.mode='managed' AND c.runtime_schema_version=2 AND c.publication_id IS NOT NULL
ON CONFLICT(company_id,binding_id) DO UPDATE SET mode_epoch=excluded.mode_epoch,publication_id=excluded.publication_id;
-- name: ClaimTaskControlV2Job :one
SELECT * FROM task_control_v2_jobs ORDER BY queued_at LIMIT 1 FOR UPDATE SKIP LOCKED;
-- name: DeleteTaskControlV2Job :exec
DELETE FROM task_control_v2_jobs WHERE company_id=$1 AND binding_id=$2;
-- name: PruneTaskControlV2History :exec
DELETE FROM task_control_v2_acks WHERE received_at<now()-interval '30 days';
-- name: PruneTaskControlV2Issued :exec
DELETE FROM task_control_v2_issued WHERE issued_at<now()-interval '30 days';
-- name: PruneTaskControlV2Snapshots :exec
DELETE FROM task_control_v2_snapshots s WHERE created_at<now()-interval '30 days' AND NOT EXISTS (SELECT 1 FROM task_control_v2_state c WHERE c.company_id=s.company_id AND c.current_snapshot_id=s.id) AND NOT EXISTS(SELECT 1 FROM task_control_v2_issued i WHERE i.company_id=s.company_id AND i.snapshot_id=s.id);
-- name: EnsureTaskControlV2ReferenceRevision :exec
INSERT INTO task_control_reference_revisions(company_id) VALUES($1) ON CONFLICT DO NOTHING;
-- name: LockTaskControlV2ReferenceRevision :one
SELECT organization_revision,mapping_revision FROM task_control_reference_revisions WHERE company_id=$1 FOR SHARE;

-- name: DrainTaskControlV2SourceChanges :exec
WITH consumed AS (
 DELETE FROM task_control_v2_source_changes
 WHERE id IN (SELECT id FROM task_control_v2_source_changes ORDER BY id LIMIT 1000 FOR UPDATE SKIP LOCKED)
 RETURNING company_id
)
INSERT INTO task_control_v2_jobs(company_id,binding_id,mode_epoch,publication_id)
 SELECT c.company_id,c.id,c.mode_epoch,c.publication_id
 FROM task_control_connections c
 WHERE c.company_id IN (SELECT company_id FROM consumed)
 AND c.state='active' AND c.mode='managed' AND c.runtime_schema_version=2 AND c.publication_id IS NOT NULL
 ON CONFLICT(company_id,binding_id) DO NOTHING;
-- name: ListTaskControlPublicationsV2 :many
SELECT outer_p.* FROM task_control_publications outer_p WHERE outer_p.company_id=$1 AND outer_p.binding_id=$2 AND outer_p.payload->>'schemaVersion'='2'
AND (sqlc.narg(cursor_id)::uuid IS NULL OR outer_p.version<(SELECT p.version FROM task_control_publications p WHERE p.company_id=$1 AND p.binding_id=$2 AND p.id=sqlc.narg(cursor_id)))
ORDER BY outer_p.version DESC LIMIT sqlc.arg(page_limit)::integer;
-- name: HasCurrentTaskControlV2Ack :one
SELECT EXISTS(SELECT 1 FROM task_control_v2_acks WHERE company_id=$1 AND binding_id=$2 AND mode_epoch=$3 AND runtime_revision=$4 AND received_at>=sqlc.arg(fresh_after)::timestamptz);
-- name: RecordTaskControlV2Failure :exec
INSERT INTO task_control_v2_failures(company_id,binding_id,code,message) VALUES($1,$2,$3,$4) ON CONFLICT(company_id,binding_id) DO UPDATE SET code=excluded.code,message=excluded.message,updated_at=now();
-- name: ClearTaskControlV2Failure :exec
DELETE FROM task_control_v2_failures WHERE company_id=$1 AND binding_id=$2;
-- name: GetTaskControlV2Failure :one
SELECT * FROM task_control_v2_failures WHERE company_id=$1 AND binding_id=$2;
