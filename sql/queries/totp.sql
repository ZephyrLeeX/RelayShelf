-- name: GetUserTOTP :one
SELECT * FROM user_totp WHERE user_id = $1;

-- name: UpsertPendingTOTP :execrows
INSERT INTO user_totp (id, user_id, secret_ciphertext, secret_nonce, secret_encryption_version, digits, period_seconds, algorithm, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
ON CONFLICT (user_id) DO UPDATE
SET secret_ciphertext = EXCLUDED.secret_ciphertext,
    secret_nonce = EXCLUDED.secret_nonce,
    secret_encryption_version = EXCLUDED.secret_encryption_version,
    digits = EXCLUDED.digits,
    period_seconds = EXCLUDED.period_seconds,
    algorithm = EXCLUDED.algorithm,
    enabled_at = NULL,
    last_used_step = NULL,
    failed_attempts = 0,
    locked_until = NULL,
    created_at = EXCLUDED.created_at,
    updated_at = EXCLUDED.updated_at
WHERE user_totp.enabled_at IS NULL;

-- name: ConfirmTOTP :execrows
UPDATE user_totp
SET enabled_at = $2, updated_at = $2
WHERE user_id = $1 AND enabled_at IS NULL;

-- name: DeleteUserTOTP :execrows
DELETE FROM user_totp WHERE user_id = $1 AND enabled_at IS NOT NULL;

-- name: RecordTOTPSuccess :exec
UPDATE user_totp
SET last_used_step = $3, failed_attempts = 0, locked_until = NULL, updated_at = $2
WHERE user_id = $1;

-- name: ClaimTOTPStep :execrows
UPDATE user_totp
SET last_used_step = $3, failed_attempts = 0, locked_until = NULL, updated_at = $2
WHERE user_id = $1
  AND enabled_at IS NOT NULL
  AND (last_used_step IS NULL OR last_used_step < $3);

-- name: RecordTOTPFailure :exec
UPDATE user_totp
SET failed_attempts = failed_attempts + 1,
    locked_until = CASE WHEN failed_attempts + 1 >= $3 THEN $4 ELSE locked_until END,
    updated_at = $2
WHERE user_id = $1;

-- name: CreateTOTPChallenge :exec
INSERT INTO totp_challenges (id, user_id, device_id, token_hash, expires_at, created_at, pending_device_name, pending_user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetTOTPChallengeByHash :one
SELECT * FROM totp_challenges WHERE token_hash = $1;

-- name: ConsumeTOTPChallenge :execrows
UPDATE totp_challenges
SET consumed_at = $2
WHERE id = $1 AND user_id = $3 AND consumed_at IS NULL AND expires_at > $2;

-- name: BumpTOTPChallengeAttempts :exec
UPDATE totp_challenges SET attempts = attempts + 1 WHERE id = $1;

-- name: DeleteExpiredTOTPChallenges :execrows
WITH doomed AS (
  SELECT c.id
  FROM totp_challenges AS c
  WHERE c.expires_at < $1 OR (c.consumed_at IS NOT NULL AND c.consumed_at < $1)
  ORDER BY c.expires_at, c.id
  LIMIT $2
  FOR UPDATE SKIP LOCKED
)
DELETE FROM totp_challenges
WHERE id IN (SELECT id FROM doomed);

-- name: CountActiveAdmins :one
SELECT count(*)::int FROM users WHERE status = 'ACTIVE' AND is_admin;

-- name: CountActiveAdminsWithoutTOTP :one
SELECT count(*)::int FROM users u
WHERE u.status = 'ACTIVE' AND u.is_admin
  AND NOT EXISTS (
    SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.enabled_at IS NOT NULL
  );
