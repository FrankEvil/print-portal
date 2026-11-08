-- Persist the PPD-specific extra options selected in the web print form.
ALTER TABLE jobs ADD COLUMN ppd_options TEXT NOT NULL DEFAULT '{}';
