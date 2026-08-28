-- Phase 11 review fixes: keep pre-2FA device data in short-lived challenge
-- state, and support bounded retention of consumed challenges.

ALTER TABLE totp_challenges
    ADD COLUMN pending_device_name TEXT NOT NULL DEFAULT 'New device'
        CHECK (char_length(pending_device_name) BETWEEN 1 AND 100),
    ADD COLUMN pending_user_agent TEXT NOT NULL DEFAULT ''
        CHECK (char_length(pending_user_agent) <= 512);

CREATE INDEX totp_challenges_consumed_idx
    ON totp_challenges(consumed_at)
    WHERE consumed_at IS NOT NULL;
