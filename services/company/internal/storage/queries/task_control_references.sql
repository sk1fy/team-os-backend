-- name: GetTaskControlReferenceRevisions :one
SELECT COALESCE((SELECT organization_revision FROM task_control_reference_revisions r WHERE r.company_id=sqlc.arg(company_id)),1)::bigint AS organization_revision,
 COALESCE((SELECT mapping_revision FROM task_control_reference_revisions r WHERE r.company_id=sqlc.arg(company_id)),1)::bigint AS mapping_revision;
-- name: TaskControlReferenceEmployees :one
SELECT COALESCE(jsonb_agg(e ORDER BY e->>'name',e->>'employeeId'),'[]'::jsonb)::jsonb AS employees FROM (
 SELECT jsonb_build_object('employeeId',u.id,'name',trim(u.first_name||' '||COALESCE(u.last_name,'')),
 'positionName',COALESCE((SELECT string_agg(p.name, ', ' ORDER BY p.name) FROM positions p JOIN user_positions up ON up.company_id=p.company_id AND up.position_id=p.id WHERE up.company_id=u.company_id AND up.user_id=u.id),''),
 'departmentIds',COALESCE((SELECT jsonb_agg(d.department_id ORDER BY d.department_id) FROM (SELECT ud.department_id FROM user_departments ud WHERE ud.company_id=u.company_id AND ud.user_id=u.id UNION SELECT p.department_id FROM positions p JOIN user_positions up ON up.company_id=p.company_id AND up.position_id=p.id WHERE up.company_id=u.company_id AND up.user_id=u.id) d),'[]'::jsonb),
 'crmUserIds',COALESCE((SELECT jsonb_agg(m.crm_user_id ORDER BY m.crm_user_id) FROM task_control_confirmed_employee_mappings m WHERE m.company_id=u.company_id AND m.account_id=$2 AND m.employee_id=u.id),'[]'::jsonb),
 'active',u.status='active' AND u.external_deleted_at IS NULL) AS e FROM users u WHERE u.company_id=$1
) entries;
-- name: TaskControlReferenceDepartments :one
SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'name',name,'parentId',parent_id) ORDER BY name,id),'[]'::jsonb)::jsonb AS departments FROM departments WHERE company_id=$1;
-- name: GetTaskControlReferenceCache :one
SELECT * FROM task_control_reference_cache WHERE company_id=$1 AND binding_id=$2 AND scope_key=$3;
-- name: SaveTaskControlReferenceCache :exec
INSERT INTO task_control_reference_cache(company_id,binding_id,scope_key,payload,last_error) VALUES($1,$2,$3,sqlc.narg(payload),sqlc.narg(last_error))
ON CONFLICT(company_id,binding_id,scope_key) DO UPDATE SET payload=COALESCE(excluded.payload,task_control_reference_cache.payload),last_error=excluded.last_error,updated_at=now()
WHERE excluded.payload IS NULL OR task_control_reference_cache.payload IS NULL OR (task_control_reference_cache.payload->'provenance'->>'fetchStartedAt')::timestamptz <= (excluded.payload->'provenance'->>'fetchStartedAt')::timestamptz;
-- name: QueueTaskControlReferenceRefresh :one
INSERT INTO task_control_reference_refreshes(id,company_id,binding_id,scope_key) VALUES($1,$2,$3,$4)
ON CONFLICT(company_id,binding_id,scope_key) WHERE state IN ('pending','running') DO UPDATE SET scope_key=excluded.scope_key RETURNING *;
-- name: GetTaskControlReferenceRefresh :one
SELECT * FROM task_control_reference_refreshes WHERE company_id=$1 AND binding_id=$2 AND scope_key=$3 AND id=$4;
-- name: ClaimTaskControlReferenceRefresh :one
UPDATE task_control_reference_refreshes SET state='running',lease_until=now()+interval '45 seconds'
WHERE id=(SELECT id FROM task_control_reference_refreshes WHERE state='pending' OR (state='running' AND lease_until<now()) ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING *;
-- name: CompleteTaskControlReferenceRefresh :exec
UPDATE task_control_reference_refreshes SET state=$2,error_code=sqlc.narg(error_code),completed_at=now(),lease_until=NULL WHERE id=$1;
-- name: TaskControlReferenceActiveConnections :many
SELECT c.* FROM task_control_connections c JOIN companies co ON co.id=c.company_id AND co.status='active'
WHERE c.state='active' ORDER BY c.company_id,c.id;
-- name: PruneTaskControlReferenceRefreshes :exec
DELETE FROM task_control_reference_refreshes WHERE id IN (SELECT id FROM task_control_reference_refreshes WHERE state IN ('completed','failed') AND completed_at<now()-interval '7 days' ORDER BY completed_at LIMIT 1000);
-- name: RecordTaskControlEmployeeMapping :exec
INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;
-- name: ClearTaskControlEmployeeMapping :exec
DELETE FROM task_control_employee_mappings WHERE company_id=$1 AND account_id=$2 AND employee_id=$3 AND crm_user_id<>$4;
-- name: LatestTaskControlReferenceRefresh :one
SELECT * FROM task_control_reference_refreshes WHERE company_id=$1 AND binding_id=$2 AND scope_key=$3 ORDER BY created_at DESC LIMIT 1;
-- name: LockTaskControlReferenceRefresh :one
SELECT * FROM task_control_reference_refreshes WHERE id=$1 FOR UPDATE;
-- name: InvalidateTaskControlReferenceCache :exec
UPDATE task_control_reference_cache SET payload=NULL,last_error=$4,updated_at=now() WHERE company_id=$1 AND binding_id=$2 AND scope_key=$3;
-- name: TaskControlReferenceOrganizationCounts :one
SELECT (SELECT count(*) FROM users u WHERE u.company_id=sqlc.arg(company_id))::bigint AS employees,
 (SELECT count(*) FROM departments d WHERE d.company_id=sqlc.arg(company_id))::bigint AS departments;
