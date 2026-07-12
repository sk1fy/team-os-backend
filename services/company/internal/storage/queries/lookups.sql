-- name: GetUsersByIDs :many
SELECT u.*,
       COALESCE(array_agg(up.position_id) FILTER (WHERE up.position_id IS NOT NULL), '{}')::uuid[] AS position_ids
FROM users AS u
LEFT JOIN user_positions AS up
  ON up.company_id = u.company_id
 AND up.user_id = u.id
WHERE u.company_id = sqlc.arg('company_id')
  AND u.id = ANY(sqlc.arg('user_ids')::uuid[])
GROUP BY u.id
ORDER BY array_position(sqlc.arg('user_ids')::uuid[], u.id);

-- name: ResolvePositionUserIDs :many
SELECT u.id
FROM users AS u
JOIN user_positions AS up
  ON up.company_id = u.company_id
 AND up.user_id = u.id
WHERE u.company_id = sqlc.arg('company_id')
  AND up.position_id = sqlc.arg('position_id')
  AND u.status = 'active'
ORDER BY u.id;

-- name: ResolveDepartmentUserIDs :many
WITH RECURSIVE selected_departments AS (
    SELECT d.id
    FROM departments AS d
    WHERE d.company_id = sqlc.arg('company_id')
      AND d.id = sqlc.arg('department_id')

    UNION

    SELECT child.id
    FROM departments AS child
    JOIN selected_departments AS parent ON child.parent_id = parent.id
    WHERE child.company_id = sqlc.arg('company_id')
      AND sqlc.arg('include_descendants')::boolean
)
SELECT DISTINCT u.id
FROM users AS u
JOIN user_positions AS up
  ON up.company_id = u.company_id
 AND up.user_id = u.id
JOIN positions AS p
  ON p.company_id = up.company_id
 AND p.id = up.position_id
JOIN selected_departments AS d ON d.id = p.department_id
WHERE u.company_id = sqlc.arg('company_id')
  AND u.status = 'active'
ORDER BY u.id;
