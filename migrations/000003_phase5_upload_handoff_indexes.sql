CREATE INDEX IF NOT EXISTS upload_sessions_completed_cleanup_idx
    ON upload_sessions (completed_at, id)
    WHERE status = 'COMPLETED';

CREATE INDEX IF NOT EXISTS upload_sessions_handoff_file_idx
    ON upload_sessions (file_object_id, completed_at)
    WHERE status = 'COMPLETED'
      AND consumed_at IS NULL;
