# Print Portal 数据模型（P1）

## 1. 设计原则

- 单用户：没有 `users` 表
- 一台物理打印机对应一个 `printers` 记录
- 每个记录同时保存 PPD 队列和 RAW 队列
- 所有打印/扫描任务都记录在本地 SQLite
- 设置使用 key/value 存储，便于扩展

## 2. 表结构

### printers

```sql
CREATE TABLE printers (
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
```

### jobs

```sql
CREATE TABLE jobs (
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
```

状态：

```text
pending / converting / submitted / queued / printing / completed / failed / cancelled
```

### settings

```sql
CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

建议键：

```text
admin_token_hash
web_port
max_upload_mb
allowed_extensions
temp_retention_hours
job_history_days
default_printer_id
language
theme
```

### discovered_devices

```sql
CREATE TABLE discovered_devices (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  kind        TEXT NOT NULL,          -- printer / scanner / mfp
  name        TEXT NOT NULL,
  model       TEXT NOT NULL DEFAULT '',
  device_uri  TEXT NOT NULL DEFAULT '',
  ip          TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL,          -- usb / mdns / ipp / escl
  metadata    TEXT NOT NULL DEFAULT '{}',
  last_seen   TEXT NOT NULL
);
```

### scan_jobs

```sql
CREATE TABLE scan_jobs (
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
```

## 3. 迁移

- 使用 `goose` 或 `golang-migrate`
- 迁移目录：`migrations/`
- 启动时自动执行（仅限单进程部署）
