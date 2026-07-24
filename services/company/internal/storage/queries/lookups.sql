-- name: GetUsersByIDs :many
SELECT u.*,
       COALESCE(array_agg(up.position_id) FILTER (WHERE up.position_id IS NOT NULL), '{}')::uuid[] AS position_ids,
       CASE
           WHEN EXISTS (SELECT 1 FROM access_links access WHERE access.company_id = u.company_id AND access.user_id = u.id) THEN 'link'
           WHEN EXISTS (SELECT 1 FROM credentials credential WHERE credential.company_id = u.company_id AND credential.user_id = u.id) THEN 'password'
           ELSE 'none'
       END::text AS access_mode
FROM users AS u
LEFT JOIN user_positions AS up
  ON up.company_id = u.company_id
 AND up.user_id = u.id
WHERE u.company_id = sqlc.arg('company_id')
  AND u.id = ANY(sqlc.arg('user_ids')::uuid[])
GROUP BY u.id
ORDER BY array_position(sqlc.arg('user_ids')::uuid[], u.id);

-- name: ResolveReportUserScope :many
SELECT u.id,
       CASE
           WHEN sqlc.narg('search')::text IS NULL THEN false
           ELSE u.email ILIKE '%' || sqlc.narg('search')::text || '%'
             OR u.first_name ILIKE '%' || sqlc.narg('search')::text || '%'
             OR COALESCE(u.last_name, '') ILIKE '%' || sqlc.narg('search')::text || '%'
       END AS matches_search
FROM users AS u
WHERE u.company_id = sqlc.arg('company_id')
  AND (
      (sqlc.narg('position_id')::uuid IS NULL
       AND sqlc.narg('department_id')::uuid IS NULL)
      OR EXISTS (
          SELECT 1
          FROM user_positions AS up
          JOIN positions AS position
            ON position.company_id = up.company_id
           AND position.id = up.position_id
          WHERE up.company_id = u.company_id
            AND up.user_id = u.id
            AND (sqlc.narg('position_id')::uuid IS NULL
                 OR position.id = sqlc.narg('position_id')::uuid)
            AND (sqlc.narg('department_id')::uuid IS NULL
                 OR position.department_id = sqlc.narg('department_id')::uuid)
      )
  )
ORDER BY u.id;

-- name: GetReportUserProfiles :many
SELECT u.id AS user_id, u.email, u.first_name, u.last_name,
       COALESCE(selected_org.position_name, '')::text AS position_name,
       selected_org.department_name
FROM users AS u
LEFT JOIN LATERAL (
    SELECT position.name AS position_name,
           department.name AS department_name
    FROM user_positions AS up
    JOIN positions AS position
      ON position.company_id = up.company_id
     AND position.id = up.position_id
    LEFT JOIN departments AS department
      ON department.company_id = position.company_id
     AND department.id = position.department_id
    WHERE up.company_id = u.company_id
      AND up.user_id = u.id
      AND (sqlc.narg('preferred_position_id')::uuid IS NULL
           OR position.id = sqlc.narg('preferred_position_id')::uuid)
      AND (sqlc.narg('preferred_department_id')::uuid IS NULL
           OR position.department_id = sqlc.narg('preferred_department_id')::uuid)
    ORDER BY position.id
    LIMIT 1
) AS selected_org ON true
WHERE u.company_id = sqlc.arg('company_id')
  AND u.id = ANY(sqlc.arg('user_ids')::uuid[])
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
