CREATE TABLE IF NOT EXISTS local_job_names (
    cups_job_id TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
