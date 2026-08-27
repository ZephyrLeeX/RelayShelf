ALTER TABLE background_jobs
ADD CONSTRAINT background_jobs_thumbnail_subject_check
CHECK (
    job_type <> 'GENERATE_THUMBNAIL'
    OR (subject_type = 'FILE_OBJECT' AND subject_id IS NOT NULL)
);

CREATE UNIQUE INDEX background_jobs_thumbnail_subject_unique_idx
ON background_jobs(subject_id)
WHERE job_type = 'GENERATE_THUMBNAIL'
  AND subject_type = 'FILE_OBJECT'
  AND subject_id IS NOT NULL;

CREATE INDEX file_derivatives_status_updated_idx
ON file_derivatives(status, updated_at);
