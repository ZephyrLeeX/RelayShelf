-- name: GetUploadSettings :one
SELECT max_file_size_bytes, upload_retention_hours FROM system_settings WHERE id = 1;

-- name: LockUploadReservation :exec
SELECT pg_advisory_xact_lock(2026082504::bigint);

-- name: ActiveUploadReservation :one
SELECT COALESCE(SUM(expected_size), 0)::bigint AS total_bytes
FROM upload_sessions
WHERE status IN ('CREATED', 'UPLOADING', 'COMPLETING', 'FAILED');

-- name: ActiveUploadRemaining :one
SELECT COALESCE(SUM(GREATEST(u.expected_size - COALESCE(p.completed_bytes, 0), 0)), 0)::bigint AS remaining_bytes
FROM upload_sessions u
LEFT JOIN (
  SELECT upload_session_id, SUM(size_bytes)::bigint AS completed_bytes
  FROM upload_parts GROUP BY upload_session_id
) p ON p.upload_session_id = u.id
WHERE u.status IN ('CREATED', 'UPLOADING', 'COMPLETING', 'FAILED');

-- name: CreateUploadSession :one
INSERT INTO upload_sessions (id, user_id, original_filename, expected_size, client_mime, chunk_size, status, expires_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'CREATED', $7, $8, $8)
RETURNING *;

-- name: GetOwnedUploadSession :one
SELECT * FROM upload_sessions WHERE id = $1 AND user_id = $2;

-- name: LockOwnedUploadSession :one
SELECT * FROM upload_sessions WHERE id = $1 AND user_id = $2 FOR UPDATE;

-- name: LockUploadSessionForMaintenance :one
SELECT * FROM upload_sessions WHERE id = $1 FOR UPDATE;

-- name: GetActiveUploadSession :one
SELECT * FROM upload_sessions WHERE id = $1 AND status IN ('CREATED','UPLOADING','COMPLETING','FAILED');

-- name: ListCompletedParts :many
SELECT * FROM upload_parts WHERE upload_session_id = $1 ORDER BY part_number ASC;

-- name: InvalidateUploadPart :exec
DELETE FROM upload_parts WHERE upload_session_id = $1 AND part_number = $2;

-- name: UpsertCompletedPart :exec
INSERT INTO upload_parts (upload_session_id, part_number, size_bytes, completed_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (upload_session_id, part_number) DO UPDATE
SET size_bytes = EXCLUDED.size_bytes, completed_at = EXCLUDED.completed_at;

-- name: TransitionUploadToUploading :execrows
UPDATE upload_sessions SET status = 'UPLOADING', updated_at = $3
WHERE id = $1 AND user_id = $2 AND status IN ('CREATED','UPLOADING');

-- name: TransitionUploadToCompleting :execrows
UPDATE upload_sessions SET status = 'COMPLETING', updated_at = $3
WHERE id = $1 AND user_id = $2 AND status IN ('CREATED','UPLOADING');

-- name: FindExpiredUploads :many
SELECT * FROM upload_sessions
WHERE (status IN ('CREATED','UPLOADING','FAILED') AND expires_at <= $1)
   OR (status = 'EXPIRED' AND EXISTS (
     SELECT 1 FROM upload_parts p WHERE p.upload_session_id = upload_sessions.id
   ))
ORDER BY expires_at, id LIMIT $2;

-- name: MarkUploadExpired :execrows
UPDATE upload_sessions SET status = 'EXPIRED', updated_at = $2
WHERE id = $1 AND status IN ('CREATED','UPLOADING','FAILED') AND expires_at <= $2;

-- name: DeleteUploadParts :exec
DELETE FROM upload_parts WHERE upload_session_id = $1;
