-- name: GetDistributionDelivery :one
SELECT * FROM distribution_delivery_inbox WHERE consumer_id=$1 AND message_kind=$2 AND message_id=$3;
-- name: CreateDistributionDelivery :one
INSERT INTO distribution_delivery_inbox(receipt_id,consumer_id,message_kind,message_id,event_id,company_id,binding_id,binding_revision,installation_id,integration_id,account_id,lead_id,payload_hash,payload)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *;
-- name: GetDistributionBindingVersion :one
SELECT * FROM distribution_binding_versions WHERE company_id=$1 AND binding_id=$2 AND revision=$3;
-- name: ClaimDistributionDelivery :one
UPDATE distribution_delivery_inbox SET state='processing',attempts=attempts+1,lease_token=$1,lease_until=clock_timestamp()+interval '30 seconds'
WHERE receipt_id=(SELECT receipt_id FROM distribution_delivery_inbox WHERE ((state IN ('received','blocked') AND next_attempt_at<=clock_timestamp()) OR (state='processing' AND lease_until<clock_timestamp())) ORDER BY accepted_at,receipt_id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *;
-- name: LockDistributionDeliveryLease :one
SELECT * FROM distribution_delivery_inbox WHERE receipt_id=$1 AND lease_token=$2 AND state='processing' AND lease_until>clock_timestamp() FOR UPDATE;
-- name: FinishDistributionDelivery :exec
UPDATE distribution_delivery_inbox SET state=$3,error_code=$4,applied_at=now(),lease_token=NULL,lease_until=NULL WHERE receipt_id=$1 AND lease_token=$2;
-- name: RetryDistributionDelivery :exec
UPDATE distribution_delivery_inbox SET state='blocked',error_code=$3,next_attempt_at=$4,lease_token=NULL,lease_until=NULL WHERE receipt_id=$1 AND lease_token=$2;
-- name: GetDistributionEventReceipt :one
SELECT * FROM distribution_event_receipts WHERE consumer_id=$1 AND event_id=$2;
-- name: CreateDistributionEventReceipt :exec
INSERT INTO distribution_event_receipts(consumer_id,event_id,company_id,payload_hash,receipt_id) VALUES($1,$2,$3,$4,$5);
-- name: EnsureDistributionLeadHead :exec
INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING;
-- name: LockDistributionLeadHead :one
SELECT * FROM distribution_lead_heads WHERE account_id=$1 AND lead_id=$2 FOR UPDATE;
-- name: NextDistributionObservationGeneration :one
UPDATE distribution_lead_heads SET next_generation=next_generation+1 WHERE account_id=$1 AND lead_id=$2 AND company_id=$3 RETURNING next_generation;
-- name: SetDistributionDeliveryGeneration :exec
UPDATE distribution_delivery_inbox SET observation_generation=$3 WHERE receipt_id=$1 AND lease_token=$2;
-- name: UpdateDistributionLeadHead :exec
UPDATE distribution_lead_heads SET binding_id=$4,binding_revision=$5,applied_generation=$6,observation_revision=$7,last_sequence=$8,snapshot=$9,deleted=$10,current_entry_id=$11,observed_at=$12,absent=$13,absence_reason=$14,updated_at=now() WHERE account_id=$1 AND lead_id=$2 AND company_id=$3;
-- name: CreateDistributionObservedEntry :exec
INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at,event_kind,trigger_evidence,trigger_group_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17);
-- name: CancelDistributionObservedEntry :exec
UPDATE distribution_observed_entries SET state='cancelled',cancellation_reason=$2,finished_at=now() WHERE id=$1 AND state<>'cancelled';
-- name: GetDistributionOperationMirror :one
SELECT * FROM distribution_operation_mirrors WHERE operation_id=$1 FOR UPDATE;
-- name: RegisterDistributionOperationMirror :exec
INSERT INTO distribution_operation_mirrors(operation_id,company_id,binding_id,binding_revision,installation_id,integration_id,account_id,lead_id,episode_id,decision_id,rule_id,group_id,target_responsible_id,registration_payload,registered_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15);
-- name: GetDistributionMirrorVersion :one
SELECT * FROM distribution_operation_mirror_versions WHERE operation_id=$1 AND result_version=$2;
-- name: CreateDistributionMirrorVersion :exec
INSERT INTO distribution_operation_mirror_versions(operation_id,result_version,payload_hash,payload) VALUES($1,$2,$3,$4);
-- name: UpdateDistributionOperationMirror :exec
UPDATE distribution_operation_mirrors SET result_version=$2,result_hash=$3,result_payload=$4,state=$5,unfinished=$6,error_code=NULL,updated_at=now(),next_reconcile_at=now()+interval '30 seconds' WHERE operation_id=$1;
-- name: DueDistributionOperationMirrors :many
SELECT * FROM distribution_operation_mirrors WHERE unfinished AND next_reconcile_at<=clock_timestamp() ORDER BY next_reconcile_at,operation_id LIMIT $1;
-- name: RetryDistributionMirror :exec
UPDATE distribution_operation_mirrors SET reconcile_attempts=reconcile_attempts+1,error_code=$2,next_reconcile_at=$3 WHERE operation_id=$1;
-- name: WakeDistributionResultInbox :exec
UPDATE distribution_delivery_inbox SET next_attempt_at=now() WHERE message_kind='result' AND state='blocked' AND payload->'result'->>'operationId'=sqlc.arg(operation_id)::text;
-- name: ReserveDistributionMirrorReconcile :execrows
UPDATE distribution_operation_mirrors SET next_reconcile_at=clock_timestamp()+interval '30 seconds' WHERE operation_id=$1 AND unfinished AND next_reconcile_at<=clock_timestamp();
-- name: DistributionDeliveryDiagnostics :one
SELECT count(*) FILTER(WHERE state IN ('received','processing','blocked'))::bigint AS backlog,
 count(*) FILTER(WHERE state='blocked')::bigint AS blocked,
 COALESCE(extract(epoch FROM clock_timestamp()-min(accepted_at) FILTER(WHERE state IN ('received','processing','blocked'))),0)::double precision AS oldest_seconds,
 (SELECT count(*) FROM distribution_operation_mirrors WHERE unfinished)::bigint AS unfinished
FROM distribution_delivery_inbox;
-- name: PauseDistributionObservedEntry :exec
UPDATE distribution_observed_entries SET state='needs_configuration',evidence='ambiguous_reentry' WHERE id=$1 AND state<>'cancelled';
