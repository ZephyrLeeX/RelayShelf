ALTER TABLE messages
ADD COLUMN title TEXT,
ADD CONSTRAINT messages_title_valid CHECK (
  title IS NULL OR (
    char_length(title) BETWEEN 1 AND 200
    AND title = btrim(title)
  )
);

CREATE INDEX messages_title_trgm_idx
ON messages USING gin (title gin_trgm_ops)
WHERE title IS NOT NULL AND trashed_at IS NULL;
