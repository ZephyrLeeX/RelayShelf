CREATE INDEX tags_normalized_name_trgm_idx
    ON tags
    USING gin (normalized_name gin_trgm_ops);

CREATE INDEX message_tags_tag_message_idx
    ON message_tags (tag_id, message_id);

CREATE INDEX messages_owner_active_created_idx
    ON messages (owner_id, created_at DESC, id DESC)
    WHERE trashed_at IS NULL;
