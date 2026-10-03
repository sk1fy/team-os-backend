-- name: UpdateDistributionExecutionMode :exec
UPDATE distribution_rules SET execution_mode=$3,execution_epoch=execution_epoch+1,live_started_at=CASE WHEN $3='live' THEN greatest(clock_timestamp(),COALESCE(live_started_at,'-infinity'::timestamptz)+interval '1 microsecond') ELSE live_started_at END WHERE company_id=$1 AND id=$2 AND execution_mode<>$3;
-- name: DistributionRuleUnsettled :one
SELECT EXISTS(SELECT 1 FROM distribution_queue WHERE company_id=$1 AND rule_id=$2 AND NOT settled) AS busy;
-- name: DistributionRuleHasObservation :one
SELECT EXISTS(SELECT 1 FROM distribution_observation_jobs WHERE company_id=$1 AND rule_id=$2) AS used;
-- name: ScheduleDistributionObservation :exec
WITH observed_rules AS MATERIALIZED (SELECT r.* FROM distribution_rules r JOIN distribution_observed_entries e ON r.company_id=e.company_id AND r.binding_id=e.binding_id AND r.binding_revision=e.binding_revision AND r.pipeline_id=e.pipeline_id AND r.status_id=e.status_id WHERE e.id=$1 AND r.active AND r.execution_mode='observe' FOR SHARE OF r)
INSERT INTO distribution_observation_jobs(company_id,rule_id,execution_epoch,entry_id,event_id,binding_id,binding_revision,account_id,lead_id)
SELECT e.company_id,r.id,r.execution_epoch,e.id,$2,e.binding_id,e.binding_revision,e.account_id,e.lead_id FROM distribution_observed_entries e JOIN observed_rules r ON r.company_id=e.company_id AND r.binding_id=e.binding_id AND r.binding_revision=e.binding_revision AND r.pipeline_id=e.pipeline_id AND r.status_id=e.status_id WHERE e.id=$1 AND r.active AND r.execution_mode='observe' ON CONFLICT DO NOTHING;
-- name: ClaimDistributionObservation :one
UPDATE distribution_observation_jobs SET state='processing',lease_token=$1,lease_until=clock_timestamp()+interval '30 seconds',attempts=attempts+1,updated_at=clock_timestamp() WHERE id=(SELECT id FROM distribution_observation_jobs WHERE (state='pending' AND next_attempt_at<=clock_timestamp()) OR (state='processing' AND lease_until<clock_timestamp()) ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *;
-- name: LockDistributionObservation :one
SELECT * FROM distribution_observation_jobs WHERE id=$1 AND lease_token=$2 AND lease_until>clock_timestamp() FOR UPDATE;
-- name: RetryDistributionObservation :exec
UPDATE distribution_observation_jobs SET state='pending',next_attempt_at=clock_timestamp()+interval '1 minute',lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND lease_token=$2 AND lease_until>clock_timestamp();
-- name: FinishDistributionObservation :exec
UPDATE distribution_observation_jobs SET state='completed',lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND lease_token=$2 AND lease_until>clock_timestamp();
-- name: SaveDistributionObservation :exec
INSERT INTO distribution_observations(id,company_id,rule_id,group_id,execution_epoch,entry_id,event_id,binding_id,binding_revision,account_id,lead_id,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT DO NOTHING;
-- name: ListDistributionObservations :many
SELECT * FROM distribution_observations WHERE company_id=$1 AND rule_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4;
-- name: GetDistributionObservationCursor :one
SELECT cursor_order,cursor_next FROM distribution_group_claims WHERE company_id=$1 AND group_id=$2;
-- name: LatestDistributionLeadObservation :one
SELECT * FROM distribution_observations WHERE company_id=$1 AND binding_id=$2 AND lead_id=$3 ORDER BY created_at DESC,id DESC LIMIT 1;

-- name: LockDistributionRule :one
SELECT * FROM distribution_rules WHERE company_id=$1 AND id=$2 FOR UPDATE;
