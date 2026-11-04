-- Print Portal P1 initial schema
CREATE TABLE IF NOT EXISTS printers (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL,
  model         TEXT NOT NULL DEFAULT '',
  ppd_queue     TEXT NOT NULL,
  raw_queue     TEXT NOT NULL,
  device_uri    TEXT NOT NULL,
  ppd_file      TEXT NOT NULL DEFAULT '',
  is_default    INTEGER NOT NULL DEFAULT 0,
  is_shared     INTEGER NOT NULL DEFAULT 1,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  printer_id      INTEGER NOT NULL,
  filename        TEXT NOT NULL,
  source_ip       TEXT NOT NULL DEFAULT '',
  pages           INTEGER NOT NULL DEFAULT 0,
  page_range      TEXT NOT NULL DEFAULT '',
  duplex          TEXT NOT NULL DEFAULT 'one-sided',
  copies          INTEGER NOT NULL DEFAULT 1,
  paper_size      TEXT NOT NULL DEFAULT 'A4',
  orientation     TEXT NOT NULL DEFAULT 'portrait',
  color_mode      TEXT NOT NULL DEFAULT 'monochrome',
  status          TEXT NOT NULL DEFAULT 'pending',
  cups_job_id     TEXT NOT NULL DEFAULT '',
  error_message   TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  FOREIGN KEY (printer_id) REFERENCES printers(id)
);

CREATE TABLE IF NOT EXISTS settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS discovered_devices (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  kind        TEXT NOT NULL,
  name        TEXT NOT NULL,
  model       TEXT NOT NULL DEFAULT '',
  device_uri  TEXT NOT NULL DEFAULT '',
  ip          TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL,
  metadata    TEXT NOT NULL DEFAULT '{}',
  last_seen   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS scan_jobs (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  scanner     TEXT NOT NULL,
  filename    TEXT NOT NULL,
  format      TEXT NOT NULL DEFAULT 'pdf',
  resolution  INTEGER NOT NULL DEFAULT 300,
  color_mode  TEXT NOT NULL DEFAULT 'color',
  status      TEXT NOT NULL DEFAULT 'pending',
  stored_path TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
