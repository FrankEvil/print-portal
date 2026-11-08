-- Persist page layout options (N-up, borders, rotation, mirroring) per job.
ALTER TABLE jobs ADD COLUMN layout_options TEXT NOT NULL DEFAULT '{}';
