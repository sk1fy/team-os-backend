-- name: LockDistributionCompany :one
SELECT id FROM companies WHERE id=$1 FOR UPDATE;
-- name: CreateDistributionBinding :one
INSERT INTO distribution_bindings(id,company_id,installation_id,integration_id,account_id,state,intent_id,initiated_by,initiator_snapshot,expires_at)
VALUES($1,$2,$3,$4,$5,'pending',$6,$7,$7,$8) RETURNING *;
-- name: GetDistributionBinding :one
SELECT * FROM distribution_bindings WHERE company_id=$1 AND id=$2;
-- name: CurrentDistributionBinding :one
SELECT * FROM distribution_bindings WHERE company_id=$1 AND state <> 'revoked' FOR UPDATE;
-- name: ListDistributionBindings :many
SELECT * FROM distribution_bindings WHERE company_id=$1 ORDER BY created_at,id;
-- name: ActivateDistributionBinding :one
UPDATE distribution_bindings SET state='active',updated_at=now() WHERE company_id=$1 AND id=$2 AND revision=$3 AND state IN ('pending','active') RETURNING *;
-- name: RecordDistributionBindingVersion :exec
INSERT INTO distribution_binding_versions(company_id,binding_id,revision,installation_id,integration_id,account_id,state)
SELECT company_id,id,revision,installation_id,integration_id,account_id,state FROM distribution_bindings WHERE distribution_bindings.company_id=$1 AND distribution_bindings.id=$2
ON CONFLICT DO NOTHING;
-- name: ListDistributionMappings :many
SELECT m.* FROM distribution_employee_mappings m WHERE m.company_id=$1 AND m.binding_id=$2 ORDER BY m.user_id;
-- name: UpsertDistributionMapping :one
INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at)
VALUES($1,$2,$3,$4,$4,$5,$6,$7)
ON CONFLICT(company_id,binding_id,user_id) DO UPDATE SET crm_user_id=EXCLUDED.crm_user_id,state=EXCLUDED.state,revision=distribution_employee_mappings.revision+1,verified_at=EXCLUDED.verified_at
RETURNING *;
-- name: GetDistributionMappingByCRM :one
SELECT m.*,u.status AS user_status,u.role AS user_role,ARRAY(SELECT a.section FROM employee_section_access a WHERE a.company_id=u.company_id AND a.user_id=u.id)::text[] AS section_access FROM distribution_employee_mappings m
JOIN users u ON u.company_id=m.company_id AND u.id=m.user_id WHERE m.company_id=$1 AND m.binding_id=$2 AND m.crm_user_id=$3 AND u.external_deleted_at IS NULL;
-- name: ClaimDistributionNonce :execrows
INSERT INTO distribution_service_nonces(key_id,nonce,expires_at) VALUES($1,$2,$3) ON CONFLICT DO NOTHING;
-- name: CleanupDistributionNonces :exec
DELETE FROM distribution_service_nonces WHERE expires_at < now();

-- name: GetDistributionServiceGrant :one
SELECT active FROM distribution_service_grants WHERE key_id=$1 AND company_id=$2 AND installation_id=$3 AND capability='widget-access';
-- name: RevokeDistributionBinding :one
UPDATE distribution_bindings SET state='revoked',revision=revision+1,updated_at=now() WHERE company_id=$1 AND id=$2 AND state <> 'revoked' RETURNING *;

-- name: NextDistributionMappingRevision :one
UPDATE distribution_bindings SET mapping_revision=mapping_revision+1 WHERE company_id=$1 AND id=$2 AND state='active' RETURNING mapping_revision;
-- name: StoreDistributionMappingSnapshot :exec
INSERT INTO distribution_mapping_snapshots(company_id,binding_id,revision,payload) VALUES($1,$2,$3,$4);
-- name: GetDistributionMappingSnapshot :one
SELECT payload FROM distribution_mapping_snapshots WHERE company_id=$1 AND binding_id=$2 AND revision=$3;
-- name: AckDistributionMappingSnapshot :execrows
UPDATE distribution_bindings SET mapping_ack_revision=$3 WHERE company_id=$1 AND id=$2 AND mapping_revision=$3 AND state='active';
-- name: ListDistributionIdentityCandidates :many
SELECT u.id AS user_id,u.status,legacy.crm_user_id FROM users u
JOIN (
 SELECT lu.id AS user_id,lu.external_id AS crm_user_id FROM users lu WHERE lu.company_id=sqlc.arg(company_id) AND lu.source='amo' AND lu.external_id IS NOT NULL
 AND EXISTS(SELECT 1 FROM companies lc WHERE lc.id=sqlc.arg(company_id) AND lc.amo_account_id=sqlc.arg(account_id))
 UNION
 SELECT xi.user_id,xi.external_user_id AS crm_user_id FROM user_external_identities xi
 WHERE xi.company_id=sqlc.arg(company_id) AND xi.external_account_id=sqlc.arg(account_id) AND xi.provider IN ('rakurs','amocrm')
) legacy ON legacy.user_id=u.id
WHERE u.company_id=sqlc.arg(company_id) AND u.external_deleted_at IS NULL ORDER BY u.id,legacy.crm_user_id;
-- name: SetDistributionMappingState :exec
UPDATE distribution_employee_mappings SET state=$4,revision=revision+1,verified_at=$5 WHERE company_id=$1 AND binding_id=$2 AND id=$3 AND state <> $4;
