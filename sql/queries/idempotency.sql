-- name: LockIdempotencyClaim :exec
SELECT pg_advisory_xact_lock(hashtextextended($1,0));

-- name: GetIdempotencyClaim :one
SELECT request_hash,resource_id,expires_at FROM idempotency_keys
WHERE user_id=$1 AND operation=$2 AND key=$3 FOR UPDATE;

-- name: DeleteIdempotencyClaim :exec
DELETE FROM idempotency_keys WHERE user_id=$1 AND operation=$2 AND key=$3;

-- name: InsertIdempotencyResult :exec
INSERT INTO idempotency_keys(id,user_id,operation,key,request_hash,resource_type,resource_id,response_metadata,created_at,expires_at)
VALUES($1,$2,$3,$4,$5,'MESSAGE',$6,$7,$8,$9);
