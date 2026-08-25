-- name: GetOwnedMessage :one
SELECT * FROM messages WHERE id = $1 AND owner_id = $2;

-- name: GetMessageByID :one
SELECT * FROM messages WHERE id = $1;

-- name: LockOwnedMessage :one
SELECT * FROM messages WHERE id = $1 AND owner_id = $2 FOR UPDATE;

-- name: ListMessageTags :many
SELECT t.* FROM tags t JOIN message_tags mt ON mt.tag_id=t.id
WHERE mt.message_id=$1 ORDER BY t.normalized_name,t.id;

-- name: InsertMessage :exec
INSERT INTO messages(id,owner_id,body_plaintext,body_ciphertext,body_nonce,body_encryption_version,
 body_format,detected_type,detected_language,sensitive,lifecycle,is_favorite,expires_at,trashed_at,
 purge_at,source_user_id,source_message_id,created_device_id,version,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21);

-- name: SaveMessage :execrows
UPDATE messages SET body_plaintext=$3,body_ciphertext=$4,body_nonce=$5,body_encryption_version=$6,
 body_format=$7,detected_type=$8,detected_language=$9,sensitive=$10,lifecycle=$11,is_favorite=$12,
 expires_at=$13,trashed_at=$14,purge_at=$15,version=$16,updated_at=$17
WHERE id=$1 AND owner_id=$2;

-- name: GetMessageSettings :one
SELECT temporary_ttl_hours,trash_ttl_hours FROM system_settings WHERE id=1;

-- name: CountOwnedTags :one
SELECT count(*) FROM tags WHERE user_id=$1 AND id=ANY($2::uuid[]);

-- name: DeleteMessageTags :exec
DELETE FROM message_tags WHERE message_id=$1;

-- name: InsertMessageTag :exec
INSERT INTO message_tags(message_id,tag_id) VALUES($1,$2);

-- name: ListCurrentMessageTagIDs :many
SELECT tag_id FROM message_tags WHERE message_id=$1;

-- name: GetRecipientStatus :one
SELECT status FROM users WHERE id=$1;

-- name: ListActiveMessages :many
SELECT m.* FROM messages m
WHERE m.owner_id=sqlc.arg(owner_id)
 AND m.trashed_at IS NULL
 AND (m.lifecycle='PERMANENT' OR m.expires_at>sqlc.arg(now_at))
 AND (sqlc.narg(lifecycle)::text IS NULL OR m.lifecycle=sqlc.narg(lifecycle))
 AND (sqlc.narg(favorite)::boolean IS NULL OR m.is_favorite=sqlc.narg(favorite))
 AND NOT EXISTS (
   SELECT 1 FROM unnest(sqlc.arg(tag_ids)::uuid[]) requested(tag_id)
   WHERE NOT EXISTS (
     SELECT 1 FROM message_tags mt JOIN tags t ON t.id=mt.tag_id
     WHERE mt.message_id=m.id AND t.user_id=m.owner_id AND t.id=requested.tag_id))
 AND (sqlc.narg(cursor_at)::timestamptz IS NULL OR (m.created_at,m.id)<(sqlc.narg(cursor_at),sqlc.narg(cursor_id)::uuid))
ORDER BY m.created_at DESC,m.id DESC LIMIT sqlc.arg(row_limit);

-- name: ListTrashedMessages :many
SELECT m.* FROM messages m
WHERE m.owner_id=sqlc.arg(owner_id) AND m.trashed_at IS NOT NULL
 AND (sqlc.narg(cursor_at)::timestamptz IS NULL OR (m.trashed_at,m.id)<(sqlc.narg(cursor_at),sqlc.narg(cursor_id)::uuid))
ORDER BY m.trashed_at DESC,m.id DESC LIMIT sqlc.arg(row_limit);

-- name: InsertPermanentDeleteAudit :exec
INSERT INTO audit_logs(id,actor_user_id,event_type,target_type,target_id,ip,user_agent,device_id,session_id,trace_id,metadata,created_at)
VALUES($1,$2,'MESSAGE_PERMANENTLY_DELETED','MESSAGE',$3,NULLIF($4,'')::inet,$5,
 NULLIF($6::text,'00000000-0000-0000-0000-000000000000')::uuid,
 NULLIF($7::text,'00000000-0000-0000-0000-000000000000')::uuid,$8,
 jsonb_build_object('lifecycle',$9::text),$10);

-- name: DeleteTrashedOwnedMessage :execrows
DELETE FROM messages WHERE id=$1 AND owner_id=$2 AND trashed_at IS NOT NULL;

-- name: ExpireDueTemporary :execrows
WITH due AS (
 SELECT id FROM messages WHERE lifecycle='TEMPORARY' AND trashed_at IS NULL AND expires_at<=$1
 ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT $2)
UPDATE messages m SET trashed_at=$1,purge_at=$3,version=version+1,updated_at=$1
FROM due WHERE m.id=due.id;
