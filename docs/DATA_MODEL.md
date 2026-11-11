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
  is_shared     INTEGER NOT NULL DEFAULT 1, -- 允许局域网访问（IPP 网关）
  allow_discovery INTEGER NOT NULL DEFAULT 0, -- 允许通过 mDNS/DNS-SD 发现
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
```

`is_shared` 表示是否允许局域网客户端通过 IPP 网关访问；`allow_discovery` 表示是否允许 CUPS/DNS-SD 广播发现。两者都开启时局域网才能自动发现并访问；关闭访问会自动关闭发现。

### drivers

```sql
CREATE TABLE drivers (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL,            -- brlaser / escpr / hplip / ...
  version       TEXT NOT NULL DEFAULT '',
  kind          TEXT NOT NULL DEFAULT 'builtin',  -- builtin / package / ppd
  source        TEXT NOT NULL DEFAULT '',         -- image / upload / download
  package_path  TEXT NOT NULL DEFAULT '',
  ppd_dir       TEXT NOT NULL DEFAULT '',
  filter_dir    TEXT NOT NULL DEFAULT '',
  state         TEXT NOT NULL DEFAULT 'missing',  -- ready / degraded / missing
  models_count  INTEGER NOT NULL DEFAULT 0,
  license       TEXT NOT NULL DEFAULT '',
  installed_at  TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
```

说明：型号索引（`lpinfo -m` / foomatic）实时读取并缓存，不落库，避免数千条型号数据。

### jobs

```sql
CREATE TABLE jobs (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  printer_id      INTEGER NOT NULL,
  source          TEXT NOT NULL DEFAULT 'web',   -- web / local
  filename        TEXT NOT NULL,
  source_ip       TEXT NOT NULL DEFAULT '',
  cups_user       TEXT NOT NULL DEFAULT '',
  pages           INTEGER NOT NULL DEFAULT 0,
  page_range      TEXT NOT NULL DEFAULT '',
  page_parity     TEXT NOT NULL DEFAULT 'all',   -- all / odd / even
  output_order    TEXT NOT NULL DEFAULT 'normal', -- normal / reverse
  duplex          TEXT NOT NULL DEFAULT 'one-sided',
  copies          INTEGER NOT NULL DEFAULT 1,
  paper_size      TEXT NOT NULL DEFAULT 'A4',
  orientation     TEXT NOT NULL DEFAULT 'portrait',
  color_mode      TEXT NOT NULL DEFAULT 'monochrome',
  scaling         TEXT NOT NULL DEFAULT 'auto',
  ppd_options     TEXT NOT NULL DEFAULT '{}',  -- PPD 额外选项，JSON
  layout_options  TEXT NOT NULL DEFAULT '{}',  -- 布局选项，JSON
  print_mode      TEXT NOT NULL DEFAULT 'document', -- document / idcard / invoice / photos / booklet
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

来源：

```text
web    网页提交，文件已上传到应用侧，可预览、可重试
local  本地客户端直连 PPD/RAW 队列，CUPS 任务同步而来，无原始文件
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
