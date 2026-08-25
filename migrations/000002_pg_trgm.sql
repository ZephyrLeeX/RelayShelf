CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX messages_body_plaintext_trgm_idx ON messages USING gin (body_plaintext gin_trgm_ops) WHERE body_plaintext IS NOT NULL AND trashed_at IS NULL;
CREATE INDEX message_attachments_filename_trgm_idx ON message_attachments USING gin (original_filename gin_trgm_ops);
