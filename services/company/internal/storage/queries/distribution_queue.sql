-- name: GetDistributionSettings :one
SELECT * FROM distribution_settings WHERE company_id=$1;
-- name: SaveDistributionSettings :one
INSERT INTO distribution_settings(company_id,timezone) VALUES($1,$2) ON CONFLICT(company_id) DO UPDATE SET timezone=EXCLUDED.timezone,revision=distribution_settings.revision+1 RETURNING *;
-- name: ListDistributionRules :many
SELECT * FROM distribution_rules WHERE company_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3;
-- name: GetDistributionRule :one
SELECT * FROM distribution_rules WHERE company_id=$1 AND id=$2;
-- name: CreateDistributionRule :one
INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id,active,keep_current,first_activation_at,live_started_at,execution_mode,source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,CASE WHEN $9 THEN clock_timestamp() ELSE NULL END,CASE WHEN $9 AND COALESCE(NULLIF(sqlc.arg(execution_mode)::text,''),'live')='live' THEN clock_timestamp() ELSE NULL END,COALESCE(NULLIF(sqlc.arg(execution_mode)::text,''),'live'),COALESCE(NULLIF(sqlc.arg(source)::text,''),'legacy_stage')) RETURNING *;
-- name: UpdateDistributionRule :one
UPDATE distribution_rules SET active=$3,keep_current=$4,live_started_at=CASE WHEN $3 AND execution_mode='live' THEN COALESCE(live_started_at,clock_timestamp()) ELSE live_started_at END,first_activation_at=CASE WHEN $3 THEN COALESCE(first_activation_at,clock_timestamp()) ELSE first_activation_at END,revision=revision+1,updated_at=clock_timestamp() WHERE company_id=$1 AND id=$2 AND revision=$5 RETURNING *;
-- name: AdmitDistributionEntries :execrows
WITH live_rules AS MATERIALIZED (
 SELECT r.* FROM distribution_rules r WHERE r.execution_mode='live' AND EXISTS(SELECT 1 FROM distribution_observed_entries e WHERE e.company_id=r.company_id AND e.binding_id=r.binding_id AND e.binding_revision=r.binding_revision AND e.account_id=r.account_id AND e.pipeline_id=r.pipeline_id AND e.state='checking' AND e.source_received_at>=r.live_started_at AND e.source_occurred_at>=r.live_started_at AND e.created_at>=r.live_started_at AND ((r.source='legacy_stage' AND e.status_id=r.status_id AND e.evidence IN('created_in_stage','observed_transition')) OR (r.source='creation' AND e.event_kind='lead.created') OR (r.source='digital_pipeline' AND e.status_id=r.status_id AND e.trigger_group_id=r.group_id AND e.evidence='digital_pipeline_trigger' AND e.trigger_evidence IS NOT NULL)) AND NOT EXISTS(SELECT 1 FROM distribution_queue q WHERE q.entry_id=e.id))
 ORDER BY r.created_at,r.id LIMIT 200 FOR SHARE SKIP LOCKED
)
INSERT INTO distribution_queue(id,company_id,entry_id,rule_id,group_id,account_id,lead_id,execution_epoch)
SELECT gen_random_uuid(),chosen.company_id,chosen.id,chosen.rule_id,chosen.group_id,chosen.account_id,chosen.lead_id,chosen.execution_epoch FROM (
SELECT DISTINCT ON(e.id) e.id,e.company_id,e.account_id,e.lead_id,e.created_at,r.id AS rule_id,r.group_id,r.execution_epoch
FROM distribution_observed_entries e JOIN live_rules r ON r.company_id=e.company_id AND r.binding_id=e.binding_id AND r.binding_revision=e.binding_revision AND r.account_id=e.account_id AND r.pipeline_id=e.pipeline_id
WHERE r.execution_mode='live' AND e.state='checking' AND r.first_activation_at IS NOT NULL AND e.created_at>=r.first_activation_at AND e.source_received_at>=r.first_activation_at AND e.source_occurred_at>=r.first_activation_at AND r.live_started_at IS NOT NULL AND e.created_at>=r.live_started_at AND e.source_received_at>=r.live_started_at AND e.source_occurred_at>=r.live_started_at AND ((r.source='legacy_stage' AND e.status_id=r.status_id AND e.evidence IN('created_in_stage','observed_transition')) OR (r.source='creation' AND e.event_kind='lead.created') OR (r.source='digital_pipeline' AND e.status_id=r.status_id AND e.trigger_group_id=r.group_id AND e.evidence='digital_pipeline_trigger' AND e.trigger_evidence IS NOT NULL)) AND NOT EXISTS(SELECT 1 FROM distribution_queue q WHERE q.entry_id=e.id)
ORDER BY e.id,r.active DESC,r.first_activation_at DESC,r.id
) chosen ORDER BY chosen.created_at,chosen.id LIMIT 200
ON CONFLICT(entry_id) DO NOTHING;
-- name: ClaimDistributionQueue :one
UPDATE distribution_queue SET lease_token=$1,lease_until=clock_timestamp()+interval '30 seconds' WHERE id=(SELECT id FROM distribution_queue WHERE state NOT IN('confirmed','kept','cancelled','failed') AND (next_attempt_at<=clock_timestamp() OR EXISTS(SELECT 1 FROM distribution_observed_entries e WHERE e.id=entry_id AND e.state='cancelled')) AND (lease_until IS NULL OR lease_until<clock_timestamp()) AND NOT EXISTS(SELECT 1 FROM distribution_queue older WHERE older.company_id=distribution_queue.company_id AND older.group_id=distribution_queue.group_id AND older.state NOT IN('confirmed','kept','cancelled','failed') AND (older.next_attempt_at<=clock_timestamp() OR older.lease_until>clock_timestamp()) AND (older.created_at,older.id)<(distribution_queue.created_at,distribution_queue.id)) ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *;
-- name: LockDistributionQueue :one
SELECT * FROM distribution_queue WHERE id=$1 FOR UPDATE;
-- name: LockDistributionQueueLease :one
SELECT * FROM distribution_queue WHERE id=$1 AND lease_token=$2 AND lease_until>clock_timestamp() FOR UPDATE;
-- name: GetDistributionObservedEntry :one
SELECT * FROM distribution_observed_entries WHERE id=$1;
-- name: EnsureDistributionGroupClaim :exec
INSERT INTO distribution_group_claims(company_id,group_id) VALUES($1,$2) ON CONFLICT DO NOTHING;
-- name: LockDistributionGroupClaim :one
SELECT * FROM distribution_group_claims WHERE company_id=$1 AND group_id=$2 FOR UPDATE;
-- name: GetDistributionLeadClaim :one
SELECT * FROM distribution_lead_claims WHERE account_id=$1 AND lead_id=$2 FOR UPDATE;
-- name: ReserveDistributionLeadClaim :execrows
INSERT INTO distribution_lead_claims(account_id,lead_id,queue_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING;
-- name: ReserveDistributionGroupClaim :exec
UPDATE distribution_group_claims SET queue_id=$3,revision=revision+1 WHERE company_id=$1 AND group_id=$2;
-- name: SaveDistributionQueueDecision :exec
UPDATE distribution_queue SET operation_id=$2,decision_id=$3,command=$4,idempotency_key=$5,cancel_key=$6,reconcile_key=$7,availability_hash=$8,claim_revision=$9,planned_employee_id=$10,selection_order=$11,planned_at=clock_timestamp(),state='dispatching',reason='decision_ready',updated_at=clock_timestamp() WHERE id=$1;
-- name: UpdateDistributionQueueState :exec
WITH prior AS (SELECT d.id,d.state AS old_state,d.reason AS old_reason FROM distribution_queue d WHERE d.id=$1 AND d.lease_token=$6 AND d.lease_until>clock_timestamp() FOR UPDATE), updated AS (UPDATE distribution_queue AS dq SET state=$2,reason=$3,next_attempt_at=$4,lease_token=NULL,lease_until=NULL,cancel_requested=$5,updated_at=clock_timestamp() FROM prior p WHERE dq.id=p.id AND dq.lease_token=$6 AND dq.lease_until>clock_timestamp() RETURNING dq.id,dq.state,dq.reason) INSERT INTO distribution_queue_history(queue_id,state,reason) SELECT u.id,u.state,u.reason FROM updated u JOIN prior p ON p.id=u.id WHERE p.old_state<>u.state OR p.old_reason<>u.reason;
-- name: AddDistributionQueueHistory :exec
INSERT INTO distribution_queue_history(queue_id,state,reason,payload) VALUES($1,$2,$3,$4);
-- name: SettleDistributionQueue :execrows
UPDATE distribution_queue SET state=$2,reason=$3,settled=true,lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND NOT settled;
-- name: ReleaseDistributionLeadClaim :exec
DELETE FROM distribution_lead_claims WHERE queue_id=$1;
-- name: ReleaseDistributionGroupClaim :exec
UPDATE distribution_group_claims SET queue_id=NULL,cursor_order=$3,cursor_next=$4 WHERE company_id=$1 AND group_id=$2 AND queue_id=$5;
-- name: ListDistributionQueue :many
SELECT * FROM distribution_queue WHERE company_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3;
-- name: ListDistributionQueueHistory :many
SELECT h.* FROM distribution_queue_history h JOIN distribution_queue q ON q.id=h.queue_id WHERE q.company_id=$1 AND q.id=$2 ORDER BY h.id LIMIT $3 OFFSET $4;
-- name: DistributionMemberInputs :many
SELECT u.id,u.status,u.external_deleted_at,s.template,m.crm_user_id,m.state AS mapping_state FROM users u LEFT JOIN user_schedules s ON s.user_id=u.id AND s.company_id=u.company_id LEFT JOIN distribution_employee_mappings m ON m.user_id=u.id AND m.company_id=u.company_id AND m.binding_id=$2 WHERE u.company_id=$1 AND u.id=ANY($3::uuid[]) ORDER BY u.id;
-- name: DistributionMemberExceptions :many
SELECT * FROM shift_exceptions WHERE company_id=$1 AND user_id=ANY($2::uuid[]) AND date >= $3 AND date <= $4 ORDER BY user_id,date,id;

-- name: EnsureDistributionAvailabilityVersion :exec
INSERT INTO distribution_availability_versions(company_id) VALUES($1) ON CONFLICT DO NOTHING;
-- name: LockDistributionAvailabilityVersion :one
SELECT * FROM distribution_availability_versions WHERE company_id=$1 FOR UPDATE;
-- name: ConsumeDistributionAvailabilityWake :execrows
WITH companies AS (SELECT company_id,revision FROM distribution_availability_versions WHERE revision>processed_revision ORDER BY company_id FOR UPDATE SKIP LOCKED LIMIT 20), candidates AS (SELECT q.id,c.revision FROM distribution_queue q JOIN companies c ON c.company_id=q.company_id WHERE q.wake_revision<c.revision AND q.state NOT IN('confirmed','kept','cancelled','failed') ORDER BY q.created_at,q.id LIMIT 200), wake AS (UPDATE distribution_queue q SET next_attempt_at=clock_timestamp(),wake_revision=c.revision FROM candidates c WHERE q.id=c.id RETURNING q.id) UPDATE distribution_availability_versions v SET processed_revision=c.revision FROM companies c WHERE c.company_id=v.company_id AND NOT EXISTS(SELECT 1 FROM distribution_queue q WHERE q.company_id=c.company_id AND q.wake_revision<c.revision AND q.state NOT IN('confirmed','kept','cancelled','failed') AND NOT EXISTS(SELECT 1 FROM wake w WHERE w.id=q.id));
-- name: GetDistributionQueueByOperation :one
SELECT * FROM distribution_queue WHERE operation_id=$1;

-- name: ResetDistributionQueueDecision :exec
UPDATE distribution_queue SET operation_id=NULL,decision_id=NULL,command=NULL,idempotency_key=NULL,cancel_key=NULL,reconcile_key=NULL,availability_hash=NULL,planned_employee_id=NULL,planned_at=NULL,selection_order='{}',cancel_requested=false,state='waiting',reason=$2,next_attempt_at=clock_timestamp()+interval '5 seconds',lease_token=NULL,lease_until=NULL WHERE id=$1;

-- name: DistributionPointHasUnfinishedOperation :one
SELECT EXISTS(SELECT 1 FROM distribution_queue q JOIN distribution_rules r ON r.id=q.rule_id WHERE r.account_id=$1 AND r.pipeline_id=$2 AND r.status_id=$3 AND q.operation_id IS NOT NULL AND NOT q.settled) AS unfinished;

-- name: GetDistributionControlRequest :one
SELECT * FROM distribution_control_requests WHERE operation_id=$1 AND action=$2 AND expected_result_version=$3;
-- name: CreateDistributionControlRequest :exec
INSERT INTO distribution_control_requests(key,queue_id,operation_id,action,expected_result_version,payload) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING;

-- name: ListDistributionQueueFiltered :many
SELECT * FROM distribution_queue WHERE company_id=sqlc.arg(company_id)
AND (sqlc.narg(group_id)::uuid IS NULL OR group_id=sqlc.narg(group_id))
AND (sqlc.narg(from_time)::timestamptz IS NULL OR created_at>=sqlc.narg(from_time))
AND (sqlc.narg(to_time)::timestamptz IS NULL OR created_at<sqlc.narg(to_time))
AND (sqlc.arg(tab)::text='' OR CASE sqlc.arg(tab)::text WHEN 'waiting' THEN state='waiting' WHEN 'assigning' THEN state IN('dispatching','uncertain') WHEN 'completed' THEN state IN('confirmed','kept') WHEN 'errors' THEN state IN('requires_configuration','failed') WHEN 'cancelled' THEN state='cancelled' ELSE false END)
ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);
-- name: GetDistributionUIAction :one
SELECT * FROM distribution_ui_actions WHERE company_id=$1 AND request_id=$2;
-- name: SaveDistributionUIAction :exec
INSERT INTO distribution_ui_actions(company_id,request_id,queue_id,actor_id,payload) VALUES($1,$2,$3,$4,$5);
-- name: WakeDistributionQueueUI :exec
UPDATE distribution_queue SET next_attempt_at=clock_timestamp(),cancel_requested=$2,updated_at=clock_timestamp() WHERE id=$1;
-- name: CancelUndispatchedDistributionQueueUI :exec
UPDATE distribution_queue SET state='cancelled',reason='user_cancelled',settled=true,cancel_requested=true,updated_at=clock_timestamp() WHERE id=$1 AND operation_id IS NULL;
-- name: RetryDistributionQueueUI :exec
UPDATE distribution_queue SET operation_id=NULL,decision_id=NULL,command=NULL,idempotency_key=NULL,cancel_key=NULL,reconcile_key=NULL,availability_hash=NULL,planned_employee_id=NULL,planned_at=NULL,selection_order='{}',cancel_requested=false,state='waiting',reason='user_retry',settled=false,next_attempt_at=clock_timestamp(),lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1;
-- name: UpdateDistributionGroupConfiguration :one
UPDATE distribution_groups SET name=$3,member_ids=$4,disabled_member_ids=$5,active=$6,algorithm='round_robin',updated_at=clock_timestamp() WHERE company_id=$1 AND id=$2 AND revision=$7 RETURNING *;
-- name: ListDistributionSummaryCandidates :many
SELECT * FROM distribution_queue WHERE company_id=sqlc.arg(company_id) AND (sqlc.narg(group_id)::uuid IS NULL OR group_id=sqlc.narg(group_id)) AND (NOT settled OR state='failed' OR (state IN('confirmed','kept') AND updated_at >= sqlc.arg(day_start) AND updated_at < sqlc.arg(day_end))) ORDER BY created_at,id LIMIT 101;
-- name: DistributionRuleHasQueue :one
SELECT EXISTS(SELECT 1 FROM distribution_queue WHERE company_id=$1 AND rule_id=$2) AS used;
-- name: DistributionGroupHasUnfinishedOperation :one
SELECT EXISTS(SELECT 1 FROM distribution_queue WHERE company_id=$1 AND group_id=$2 AND operation_id IS NOT NULL AND NOT settled) AS busy;
-- name: UpdateDistributionRulePoint :one
UPDATE distribution_rules SET pipeline_id=$3,status_id=$4,active=$5,keep_current=$6,source=COALESCE(NULLIF(sqlc.arg(source)::text,''),source),execution_epoch=execution_epoch+CASE WHEN COALESCE(NULLIF(sqlc.arg(source)::text,''),source) IS DISTINCT FROM source THEN 1 ELSE 0 END,live_started_at=CASE WHEN (pipeline_id,status_id) IS DISTINCT FROM ($3,$4) OR COALESCE(NULLIF(sqlc.arg(source)::text,''),source) IS DISTINCT FROM source THEN CASE WHEN $5 AND execution_mode='live' THEN clock_timestamp() ELSE NULL END WHEN $5 AND execution_mode='live' THEN COALESCE(live_started_at,clock_timestamp()) ELSE live_started_at END,first_activation_at=CASE WHEN (pipeline_id,status_id) IS DISTINCT FROM ($3,$4) OR COALESCE(NULLIF(sqlc.arg(source)::text,''),source) IS DISTINCT FROM source THEN CASE WHEN $5 THEN clock_timestamp() ELSE NULL END WHEN $5 THEN COALESCE(first_activation_at,clock_timestamp()) ELSE first_activation_at END,revision=revision+1,updated_at=clock_timestamp() WHERE company_id=$1 AND id=$2 AND revision=$7 RETURNING *;
-- name: DistributionGroupHasRule :one
SELECT EXISTS(SELECT 1 FROM distribution_rules WHERE company_id=$1 AND group_id=$2) AS used;
