-- Print mode chosen on the print page: document, idcard or invoice.
ALTER TABLE jobs ADD COLUMN print_mode TEXT NOT NULL DEFAULT 'document';
