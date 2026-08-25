-- name: CreateUser :one
INSERT INTO users (id, username, display_name, password_hash, is_admin, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 'ACTIVE', $6, $6) RETURNING *;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: DisableUser :execrows
UPDATE users SET status = 'DISABLED', updated_at = $2 WHERE id = $1 AND status <> 'DISABLED';

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;

-- name: UpdateUserPasswordHash :execrows
UPDATE users SET password_hash = $2, updated_at = $3 WHERE id = $1;
