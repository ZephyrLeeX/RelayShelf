-- name: CreateDevice :one
INSERT INTO devices (id, user_id, name, user_agent, first_seen_at, last_seen_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5, $5, $5) RETURNING *;

-- name: GetOwnedDevice :one
SELECT * FROM devices WHERE id = $1 AND user_id = $2;

-- name: TouchDevice :exec
UPDATE devices SET last_seen_at = $3, updated_at = $3 WHERE id = $1 AND user_id = $2;

-- name: ListOwnedDevices :many
SELECT * FROM devices WHERE user_id = $1 ORDER BY last_seen_at DESC, id DESC;

-- name: RenameOwnedDevice :one
UPDATE devices SET name = $3, updated_at = $4 WHERE id = $1 AND user_id = $2 RETURNING *;

-- name: CreateSession :one
INSERT INTO sessions (id, user_id, device_id, token_hash, expires_at, absolute_expires_at, last_seen_at, last_ip, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $7) RETURNING *;

-- name: GetAuthByTokenHash :one
SELECT s.id AS session_id, s.user_id, s.device_id, s.expires_at, s.absolute_expires_at,
 s.last_seen_at AS session_last_seen_at, COALESCE(s.last_ip::text, '') AS last_ip, s.revoked_at,
 s.created_at AS session_created_at, u.username, u.display_name,
 u.is_admin, u.status AS user_status, d.name AS device_name, d.user_agent,
 d.first_seen_at, d.last_seen_at AS device_last_seen_at
FROM sessions s JOIN users u ON u.id = s.user_id
JOIN devices d ON d.id = s.device_id AND d.user_id = s.user_id
WHERE s.token_hash = $1;

-- name: TouchSession :execrows
UPDATE sessions SET last_seen_at = $2, expires_at = LEAST($3, absolute_expires_at), last_ip = $4
WHERE id = $1 AND revoked_at IS NULL;

-- name: ListOwnedSessions :many
SELECT id, device_id, created_at, last_seen_at, expires_at, absolute_expires_at, COALESCE(last_ip::text, '') AS last_ip
FROM sessions WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC, id DESC;

-- name: RevokeOwnedSession :execrows
UPDATE sessions SET revoked_at = $3 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: RevokeOtherSessions :exec
UPDATE sessions SET revoked_at = $3 WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;

-- name: InsertAuditLog :exec
INSERT INTO audit_logs (id, actor_user_id, event_type, target_type, target_id, ip, user_agent, device_id, session_id, trace_id, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);
