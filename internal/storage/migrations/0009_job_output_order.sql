-- Paper output order: normal or reverse.
ALTER TABLE jobs ADD COLUMN output_order TEXT NOT NULL DEFAULT 'normal';
