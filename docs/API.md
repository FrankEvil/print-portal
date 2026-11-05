# Print Portal API 设计（P1）

## 1. 约定

- Base URL：`http://192.168.2.30:8088`
- 公共接口：无需登录，仅局域网可访问
- 管理接口：请求头 `X-Admin-Token: <ADMIN_TOKEN>`
- 请求/响应：JSON；上传接口使用 `multipart/form-data`
- 时间：ISO 8601 / RFC3339
- 任务 ID：服务端生成的字符串或整数（P1 可用 SQLite 自增 ID）

错误响应：

```json
{
  "error": {
    "code": "invalid_file_type",
    "message": "仅支持 PDF、JPEG、PNG、WebP、Office 文档、PostScript"
  }
}
```

## 2. 公共接口

### GET `/healthz`

健康检查。

```json
{
  "status": "ok",
  "version": "0.1.0",
  "cups": "running"
}
```

### GET `/api/printers`

打印页使用，只返回 PPD 队列。

```json
{
  "printers": [
    {
      "id": 1,
      "name": "Brother DCP-7060D",
      "model": "Brother DCP-7060D",
      "queue": "Brother-7060d",
      "is_default": true,
      "state": "idle",
      "state_reasons": []
    }
  ]
}
```

### POST `/api/print`

上传并提交打印。只能提交到 PPD 队列。异步接口：校验并落盘后立即返回，转换和提交 CUPS 由后台队列执行。

`multipart/form-data` 字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `file` | file | 必填 |
| `printer_id` | int | 必填 |
| `page_range` | string | 可选，如 `1-2`、`1-5 8` |
| `duplex` | string | `one-sided` / `two-sided-long-edge` / `two-sided-short-edge` |
| `copies` | int | 默认 1 |
| `paper_size` | string | 默认 `A4` |
| `orientation` | string | `portrait` / `landscape` |
| `color_mode` | string | `monochrome` / `color` |
| `scaling` | string | `auto` / `fit` / `fill` / `none` |

响应：`202 Accepted`

```json
{
  "job_id": 123,
  "status": "pending",
  "filename": "test.pdf",
  "printer": "Brother DCP-7060D"
}
```

前端通过 `GET /api/jobs/{id}` 轮询任务状态，不阻塞等待打印完成。

### GET `/api/jobs`

当前任务和最近历史。

查询参数：`status`、`source`（`web`/`local`）、`limit`、`offset`。

任务对象包含 `source` 字段；本地任务（`local`）无原文件，详情不提供预览。

### GET `/api/jobs/{id}`

任务详情。Web 任务包含打印参数与预览地址；本地任务仅含 CUPS 元数据（队列、用户、大小、状态时间线），无文件内容。

### DELETE `/api/jobs/{id}`

取消任务（等价于 `cancel <job-id>`）。

## 3. 管理接口

### GET `/api/admin/overview`

仪表盘：

```json
{
  "cups": {
    "version": "2.4.2",
    "state": "running",
    "server_name": "localhost",
    "ports": [8631],
    "web_interface": true,
    "share_printers": true
  },
  "printers": 1,
  "active_jobs": 0,
  "completed_jobs_today": 5,
  "default_printer": "Brother DCP-7060D"
}
```

### 打印机配置

#### GET `/api/admin/printers`

返回打印机列表（P1 只有一台）：

```json
{
  "printers": [
    {
      "id": 1,
      "name": "Brother DCP-7060D",
      "model": "brother-dcp-7060d",
      "ppd_queue": "Brother-7060d",
      "raw_queue": "Brother-7060d-raw",
      "device_uri": "usb://Brother/DCP-7060D?serial=...",
      "is_default": true
    }
  ]
}
```

#### PUT `/api/admin/printers/{name}`

更新打印机配置；后端同步执行 `lpadmin`。

```json
{
  "name": "Brother DCP-7060D",
  "model": "br7060d.ppd",
  "ppd_queue": "Brother-7060d",
  "raw_queue": "Brother-7060d-raw",
  "device_uri": "usb://Brother/DCP-7060D?serial=...",
  "is_default": true
}
```

#### POST `/api/admin/printers/{name}/test`

打印测试页。

### 打印机状态/操作

- `POST /api/admin/printers`：新增（P1 单打印机，已存在时返回 `409`）
- `PUT /api/admin/printers/{name}`：修改
- `DELETE /api/admin/printers/{name}`：删除
- `POST /api/admin/printers/{name}/pause`
- `POST /api/admin/printers/{name}/resume`
- `POST /api/admin/printers/{name}/accept`
- `POST /api/admin/printers/{name}/reject`
- `POST /api/admin/printers/{name}/purge`
- `GET /api/admin/printers/{name}/ppd-options`
- `PUT /api/admin/printers/{name}/ppd-options`

### 设备发现

- `GET /api/admin/discovery/printers`
- `GET /api/admin/discovery/scanners`
- `POST /api/admin/discovery/scan`：异步扫描，body `{"scope":"network"|"usb"|"all"}`，返回 `202 {"scan_id":"...","status":"running"}`
- `GET /api/admin/discovery/scan/{id}`：查询扫描状态与动态发现的设备
- `POST /api/admin/printers/discover`

```json
{
  "discovered_id": "mdns:...",
  "name": "Brother DCP-7060D",
  "mode": "ppd+raw"
}
```

### 驱动管理

- `GET /api/admin/drivers`：驱动列表（名称、版本、类型、状态、型号数、许可证）
- `POST /api/admin/drivers/install`：上传安装驱动包（deb / tar.gz）或 PPD
- `POST /api/admin/drivers/{id}/uninstall`：卸载驱动包（内置不可卸载）
- `POST /api/admin/drivers/{id}/refresh`：重新检测状态
- `GET /api/admin/drivers/models?q=`：搜索支持型号（分页）
- `GET /api/admin/drivers/{id}/models`：某驱动支持的型号

### CUPS 控制台

- `GET /api/admin/cups/status`
- `GET /api/admin/cups/settings`
- `PUT /api/admin/cups/settings`
- `GET /api/admin/cups/config` — 读取 `cupsd.conf`/`cups-files.conf`
- `PUT /api/admin/cups/config` — 保存（需 `cupsd -t` 校验）
- `POST /api/admin/cups/reload`
- `GET /api/admin/cups/logs?lines=200`

### 任务管理

- `GET /api/admin/jobs`
- `POST /api/admin/jobs/{id}/cancel`
- `POST /api/admin/jobs/{id}/hold`
- `POST /api/admin/jobs/{id}/release`
- `POST /api/admin/jobs/{id}/restart`

### 扫描

- `GET /api/scanners`
- `POST /api/scan`（multipart 或 JSON 参数）
- `GET /api/scans/{id}`
- `GET /api/scans/{id}/download`

### 应用设置

- `GET /api/admin/settings`
- `PUT /api/admin/settings`

### 备份

- `GET /api/admin/backup`：下载配置备份
- `POST /api/admin/restore`：上传恢复

## 4. 鉴权

- 所有 `/api/admin/*` 必须携带 `X-Admin-Token`
- 错误：
  - `401` 未提供/错误 token
  - `403` 禁止访问
  - `409` 配置冲突（例如队列不存在）
  - `422` 参数校验失败

## 5. 与 CUPS 的对应关系

| API 操作 | CUPS 命令/接口 |
|---|---|
| 列打印机 | `lpstat -p -v` / IPP Get-Printers |
| 新增 PPD 队列 | `lpadmin -p ... -v ... -m ... -E` |
| 新增 RAW 队列 | `lpadmin -p ... -v ... -E` |
| 修改地址 | `lpadmin -p ... -v ...` |
| 删除 | `lpadmin -x ...` |
| 任务列表 | `lpstat -o` / IPP Get-Jobs |
| 取消任务 | `cancel <id>` |
| 暂停/恢复 | `cupsdisable` / `cupsenable` |
| 接受/拒绝 | `cupsaccept` / `cupsreject` |
| PPD 选项 | `lpoptions -p ... -l` / `lpadmin -o` |
