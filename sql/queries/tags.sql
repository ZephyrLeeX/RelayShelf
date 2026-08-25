-- name: ListOwnedTags :many
SELECT * FROM tags WHERE user_id=$1 ORDER BY normalized_name,id;

-- name: GetOwnedTag :one
SELECT * FROM tags WHERE id=$1 AND user_id=$2;

-- name: CreateOwnedTag :one
INSERT INTO tags(id,user_id,name,normalized_name,color,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$6) RETURNING *;

-- name: UpdateOwnedTag :one
UPDATE tags SET name=$3,normalized_name=$4,color=$5,updated_at=$6
WHERE id=$1 AND user_id=$2 RETURNING *;

-- name: LockTagAffectedMessages :many
SELECT m.id FROM messages m JOIN message_tags mt ON mt.message_id=m.id
WHERE mt.tag_id=$1 AND m.owner_id=$2 ORDER BY m.id FOR UPDATE OF m;

-- name: DeleteOwnedTag :execrows
DELETE FROM tags WHERE id=$1 AND user_id=$2;

-- name: BumpTagAffectedMessages :exec
UPDATE messages SET version=version+1,updated_at=$2 WHERE id=ANY($1::uuid[]) AND owner_id=$3;
