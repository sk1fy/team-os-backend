-- name: ListDistributionGroups :many
SELECT * FROM distribution_groups WHERE company_id = $1 ORDER BY created_at, id;

-- name: GetDistributionGroup :one
SELECT * FROM distribution_groups WHERE company_id = $1 AND id = $2;

-- name: GetDistributionGroupForUpdate :one
SELECT * FROM distribution_groups WHERE company_id = $1 AND id = $2 FOR UPDATE;

-- name: CreateDistributionGroup :one
INSERT INTO distribution_groups (
    id, company_id, name, description, member_ids
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateDistributionGroup :one
UPDATE distribution_groups
SET name = CASE WHEN sqlc.arg('set_name')::boolean THEN sqlc.arg('name') ELSE name END,
    description = CASE WHEN sqlc.arg('set_description')::boolean THEN sqlc.narg('description') ELSE description END,
    active = CASE WHEN sqlc.arg('set_active')::boolean THEN sqlc.arg('active') ELSE active END,
    algorithm = CASE WHEN sqlc.arg('set_algorithm')::boolean THEN sqlc.arg('algorithm') ELSE algorithm END,
    member_ids = CASE WHEN sqlc.arg('set_member_ids')::boolean THEN sqlc.arg('member_ids') ELSE member_ids END,
    disabled_member_ids = CASE WHEN sqlc.arg('set_disabled_member_ids')::boolean THEN sqlc.arg('disabled_member_ids') ELSE disabled_member_ids END,
    source = CASE WHEN sqlc.arg('set_source')::boolean THEN sqlc.arg('source') ELSE source END,
    deal_limit = CASE WHEN sqlc.arg('set_deal_limit')::boolean THEN sqlc.arg('deal_limit') ELSE deal_limit END,
    unclaimed_minutes = CASE WHEN sqlc.arg('set_unclaimed_minutes')::boolean THEN sqlc.arg('unclaimed_minutes') ELSE unclaimed_minutes END,
    updated_at = now()
WHERE company_id = sqlc.arg('company_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: DeleteDistributionGroup :execrows
DELETE FROM distribution_groups WHERE company_id = $1 AND id = $2;

-- name: ListDistributionEvents :many
SELECT * FROM distribution_events WHERE company_id = $1 AND group_id = $2 ORDER BY created_at DESC, id DESC;

-- name: CreateDistributionEvent :one
INSERT INTO distribution_events (
    id, company_id, group_id, deal_number, user_id, status
)
VALUES ($1, $2, $3, nextval('distribution_deal_numbers'), $4, 'accepted')
RETURNING *;

-- name: ResetDistributionEvents :execrows
DELETE FROM distribution_events WHERE company_id = $1 AND group_id = $2;
