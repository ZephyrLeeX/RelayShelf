-- Phase 11 TOTP second factor.
--
-- Design boundaries (PRD section 15, DATA_MODEL section 18):
--   * secrets never live in `users`; they live here, encrypted at rest with a
--     domain-separated subkey of APP_ENCRYPTION_KEY;
--   * enrollment is two-phase: a row with enabled_at IS NULL is a pending
--     enrollment that cannot gate login until a valid code confirms it;
--   * challenges are single-purpose, single-use, short-lived capabilities
--     issued only after the password already verified.

CREATE TABLE user_totp (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    secret_ciphertext BYTEA NOT NULL CHECK (octet_length(secret_ciphertext) >= 16),
    secret_nonce BYTEA NOT NULL CHECK (octet_length(secret_nonce) = 12),
    secret_encryption_version SMALLINT NOT NULL CHECK (secret_encryption_version > 0),
    digits SMALLINT NOT NULL CHECK (digits IN (6, 8)),
    period_seconds SMALLINT NOT NULL CHECK (period_seconds BETWEEN 15 AND 120),
    algorithm TEXT NOT NULL CHECK (algorithm IN ('SHA1')),
    enabled_at TIMESTAMPTZ,
    last_used_step BIGINT CHECK (last_used_step IS NULL OR last_used_step >= 0),
    failed_attempts INTEGER NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX user_totp_enabled_idx ON user_totp(user_id) WHERE enabled_at IS NOT NULL;

CREATE TABLE totp_challenges (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX totp_challenges_expires_idx ON totp_challenges(expires_at);
