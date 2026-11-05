-- Add an explicit discovery switch. is_shared remains the "allow access" flag.
ALTER TABLE printers ADD COLUMN allow_discovery INTEGER NOT NULL DEFAULT 0;
