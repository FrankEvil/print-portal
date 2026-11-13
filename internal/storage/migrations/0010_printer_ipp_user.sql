-- Username sent as requesting-user-name when submitting jobs to an IPP
-- printer that enforces a per-user ACL (e.g. a macOS shared printer).
ALTER TABLE printers ADD COLUMN ipp_user TEXT NOT NULL DEFAULT '';
