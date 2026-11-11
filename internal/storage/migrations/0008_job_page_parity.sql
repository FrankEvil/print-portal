-- Page parity filter applied after page_range: all, odd or even.
ALTER TABLE jobs ADD COLUMN page_parity TEXT NOT NULL DEFAULT 'all';
