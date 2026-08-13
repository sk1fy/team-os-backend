-- name: DeleteStaleRegistrationLoginReservations :execrows
DELETE FROM registration_login_reservations
WHERE expires_at <= sqlc.arg('now')
   OR (consumed_at IS NOT NULL AND consumed_at <= sqlc.arg('consumed_before'));

-- name: CreateRegistrationLoginReservation :one
WITH candidate AS MATERIALIZED (
    SELECT next_teamos_login() AS login
)
INSERT INTO registration_login_reservations (
    id, login, token_hash, expires_at, created_at
)
SELECT sqlc.arg('id'), candidate.login, sqlc.arg('token_hash'),
       sqlc.arg('expires_at'), sqlc.arg('created_at')
FROM candidate
WHERE NOT EXISTS (
    SELECT 1 FROM user_logins WHERE login = candidate.login
)
  AND NOT EXISTS (
    SELECT 1 FROM registration_login_reservations WHERE login = candidate.login
)
RETURNING *;

-- name: GetRegistrationLoginReservationForUpdate :one
SELECT *
FROM registration_login_reservations
WHERE token_hash = sqlc.arg('token_hash')
FOR UPDATE;

-- name: ApplyReservedUserLogin :one
UPDATE user_logins
SET login = sqlc.arg('login')
WHERE company_id = sqlc.arg('company_id')
  AND user_id = sqlc.arg('user_id')
RETURNING login;

-- name: ConsumeRegistrationLoginReservation :one
UPDATE registration_login_reservations
SET consumed_at = sqlc.arg('consumed_at')
WHERE id = sqlc.arg('id')
  AND consumed_at IS NULL
  AND expires_at > sqlc.arg('consumed_at')
RETURNING *;
