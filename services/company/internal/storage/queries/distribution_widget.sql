-- name: ClaimDistributionWidgetRequest :execrows
INSERT INTO distribution_widget_requests(company_id,request_id,binding_id,employee_id,request_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING;
-- name: GetDistributionWidgetRequest :one
SELECT * FROM distribution_widget_requests WHERE company_id=$1 AND request_id=$2;
-- name: CompleteDistributionWidgetRequest :execrows
UPDATE distribution_widget_requests SET state=$3,response=$4,error_kind=$5,error_message=$6 WHERE company_id=$1 AND request_id=$2 AND state='pending';
-- name: ListDistributionWidgetRules :many
SELECT * FROM distribution_rules WHERE company_id=$1 AND binding_id=$2 AND binding_revision=$3 ORDER BY created_at,id LIMIT $4 OFFSET $5;
-- name: ListDistributionWidgetLeadQueue :many
SELECT q.* FROM distribution_queue q JOIN distribution_rules r ON r.id=q.rule_id WHERE q.company_id=$1 AND r.binding_id=$2 AND r.binding_revision=$3 AND q.lead_id=$4 ORDER BY q.created_at DESC,q.id LIMIT $5 OFFSET $6;
-- name: DistributionWidgetGroupOtherBinding :one
SELECT EXISTS(SELECT 1 FROM distribution_rules WHERE company_id=$1 AND group_id=$2 AND binding_id<>$3);
-- name: ListDistributionWidgetEmployees :many
SELECT u.id,concat_ws(' ',u.first_name,u.last_name)::text AS name FROM distribution_employee_mappings m JOIN users u ON u.company_id=m.company_id AND u.id=m.user_id WHERE m.company_id=$1 AND m.binding_id=$2 AND m.state='verified' AND u.status='active' ORDER BY u.first_name,u.last_name,u.id;
