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
| `page_parity` | string | 可选，`all` / `odd` / `even`，在 page_range 基础上筛奇偶 |
| `output_order` | string | 可选，`normal` / `reverse`，倒序出纸 |
| `duplex` | string | `one-sided` / `two-sided-long-edge` / `two-sided-short-edge` |
| `copies` | int | 默认 1 |
| `paper_size` | string | 默认 `A4` |
| `orientation` | string | `portrait` / `landscape` |
| `color_mode` | string | `monochrome` / `color` |
| `scaling` | string | `auto` / `fit` / `fill` / `none` |
| `ppd_options` | string | 可选，PPD 额外选项 JSON，如 `{"Resolution":"600dpi","InputSlot":"Auto"}` |
| `layout_options` | string | 可选，布局选项 JSON，如 `{"pages_per_sheet":2,"direction":"lrtb","border":true,"reverse":false,"flip_horizontal":false}` |
| `print_mode` | string | 可选，`document`（默认）/ `idcard` / `invoice` / `photos` / `booklet` |
| `preview_id` | string | 可选，复用打印模式或预览会话中已上传的文件 |

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

### 打印模式

#### POST `/api/printmode/idcard`

上传身份证正面/反面（已由前端裁剪），合成一张 A4 PDF，返回预览会话 ID。

`multipart/form-data`：

| 字段 | 类型 | 说明 |
|---|---|---|
| `front` | file | 身份证正面（JPG / PNG） |
| `back` | file | 身份证反面（JPG / PNG） |
| `layout` | string | `vertical`（默认，上下）或 `horizontal`（左右） |
| `border` | string | `1` 显示裁剪虚线 |

响应：

```json
{ "preview_id": "1794379663847074000", "page_count": 1 }
```

#### POST `/api/printmode/invoice`

上传一个或多个 PDF / 图片发票，合成 A4 页面并以预览会话返回。

| 字段 | 类型 | 说明 |
|---|---|---|
| `files` | file[] | 可重复，PDF / JPG / PNG |
| `image_sizes` | string | 图片发票尺寸 JSON 数组，如 `[{"width_mm":210,"height_mm":140}]` |
| `per_sheet` | int | 每张 A4 放 1 或 2 张，默认 1 |
| `trim` | string | `0` 关闭自动裁白边，默认开启 |
| `cut_lines` | string | `1` 显示裁剪线 |

响应：

```json
{
  "preview_id": "...",
  "page_count": 2,
  "items": [{ "name": "发票.pdf", "kind": "pdf", "pages": [{ "width_mm": 210, "height_mm": 140 }] }],
  "sheets": [{ "index": 0, "items": ["发票.pdf"] }]
}
```

#### POST `/api/printmode/photos`

图片打印：多张图片排到指定纸张网格。

| 字段 | 说明 |
|---|---|
| `files` | 图片文件，可重复 |
| `paper_width_mm` / `paper_height_mm` | 纸张宽高（mm） |
| `per_sheet` | 每张纸 1/2/4/6/8 张 |
| `arrangement` | `auto` / `horizontal` / `vertical` |
| `scaling` | `fit` / `fill` |
| `borderless` | `1` 无边框 |
| `margin_mm` | 白边边距（mm） |
| `cut_lines` | `1` 显示裁剪线 |

#### POST `/api/printmode/photos/manual`

手动拼贴：图片按用户给定的纸张坐标渲染。

| 字段 | 说明 |
|---|---|
| `files` | 图片文件，可重复 |
| `paper_width_mm` / `paper_height_mm` | 纸张宽高（mm） |
| `layout` | JSON，`{ "sheets": [{ "items": [{ "index": 0, "x_mm": 10, "y_mm": 10, "w_mm": 80, "h_mm": 60, "rotation": 0 }] }] }` |

#### POST `/api/printmode/booklet/geometry`

书籍版面计算：不上传 PDF，只根据页数返回骑马钉页序，前端用 PDF.js 本地渲染预览。

请求：

```json
{
  "page_count": 38,
  "signature_pages": 0,
  "page_range": "",
  "page_parity": "all"
}
```

响应：

```json
{
  "padded_pages": 40,
  "sheet_count": 10,
  "side_count": 20,
  "sheets": [
    { "front": [0, 1], "back": [2, 0] }
  ]
}
```

`0` 表示补的空白页。

#### POST `/api/printmode/booklet`

书籍打印：PDF 按骑马钉排版，返回合订预览以及正面/反面两个打印文件。

| 字段 | 说明 |
|---|---|
| `file` | PDF 文件 |
| `paper_width_mm` / `paper_height_mm` | 展开纸张尺寸（A4 横版 297×210，A3 420×297） |
| `signature_pages` | 0=整本，或 4/8/16/32 |
| `cut_lines` | `1` 显示裁剪线 |
| `nas_path` | 可选，NAS 上的 PDF 路径；与 `file` 二选一 |

响应：

```json
{
  "preview_id": "...",
  "front_preview_id": "...",
  "back_preview_id": "...",
  "page_count": 8,
  "sheet_count": 4
}
```

#### POST `/api/printmode/inspect`

上传发票文件，只读取页面尺寸，不生成预览。

#### GET `/api/printmode/preview/{id}`

下载/预览打印模式合成的 PDF。

### POST `/api/preview/geometry`

计算版面几何，返回每张纸上有哪些页、每页在第几行第几列、是否需要旋转。

响应只有几百字节，前端拿它渲染浏览器本地的文件，因此预览不需要上传或下载文档。
打印时后端调用同一个 `layout.ComputeGeometry`，两边共用一套规则。

请求体（JSON）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `page_count` | int | 源文档页数，必填 |
| `page_aspect` | number | 源页面平均宽高比，用于挑选网格 |
| `printer_id` | int | 用于读取 PPD 可打印区域 |
| `paper_size` | string | 纸张大小 |
| `orientation` | string | `portrait` / `landscape` |
| `page_range` | string | 页码范围，空表示全部 |
| `layout_options` | object | 与打印相同的布局参数 |

响应示例：

```json
{
  "cols": 1,
  "rows": 2,
  "paper_width": 595.28,
  "paper_height": 841.89,
  "margin_left": 8,
  "margin_top": 16,
  "margin_right": 8,
  "margin_bottom": 8,
  "pages_per_sheet": 2,
  "border": false,
  "reverse": false,
  "flip_horizontal": false,
  "sheets": [
    { "index": 0, "slots": [
      { "page": 1, "col": 0, "row": 0, "rotate": 90 },
      { "page": 2, "col": 0, "row": 1, "rotate": 90 }
    ]}
  ]
}
```

### POST `/api/preview`

生成非 PDF 文件（Office 等）的单页预览图。浏览器无法直接渲染这些格式，所以仍由服务端
转换后返回图片。PDF 文件不走这个接口。

生成打印版面预览（第一页）。`multipart/form-data` 字段与 `POST /api/print` 类似，另支持：

| 字段 | 类型 | 说明 |
|---|---|---|
| `file` | file | 上传文件；与 `nas_path` 二选一 |
| `nas_path` | string | NAS 授权路径；与 `file` 二选一 |
| `paper_size` | string | 纸张大小 |
| `orientation` | string | `portrait` / `landscape` |
| `color_mode` | string | `monochrome` / `color` |
| `scaling` | string | `auto` / `fit` / `fill` / `none` |
| `page` | int | 预览页码，默认 1 |

返回 `image/png`。

### GET `/api/jobs`

当前任务和最近历史。

查询参数：`status`、`source`（`web`/`local`）、`limit`、`offset`。

任务对象包含 `source` 字段；本地任务（`local`）无原文件，详情不提供预览。

### GET `/api/jobs/{id}`

任务详情。Web 任务包含打印参数与预览地址；本地任务仅含 CUPS 元数据（队列、用户、大小、状态时间线），无文件内容。

### GET `/api/jobs/{id}/file`

返回 Web 任务实际送去打印的转换后 PDF（inline），用于核对打印内容。仅 `source=web` 且转换已完成的任务可用。

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
- `GET /api/admin/printers/{name}/maintenance`：返回驱动声明的维护能力（`maintenance.json`）
- `POST /api/admin/printers/{name}/maintenance/{action}`：执行白名单维护动作，返回输出

### 驱动维护清单

驱动包可以在根目录或任意子目录放置 `maintenance.json`，安装后自动注册：

```json
{
  "version": 1,
  "actions": [
    {
      "id": "status",
      "label": "状态 / 耗材",
      "description": "读取打印机状态",
      "command": ["brprintconf", "--status"],
      "timeout": 10,
      "confirm": false
    },
    {
      "id": "clean",
      "label": "清洗",
      "description": "执行打印机清洗",
      "command": ["brprintconf", "--clean"],
      "timeout": 60,
      "confirm": true
    }
  ]
}
```

命令以参数数组直接执行，不经过 shell；驱动没有清单时只展示通用 CUPS 状态、PPD 选项和测试页。

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

### 共享

- `GET /api/admin/sharing`：返回访问地址配置和每台打印机的 PPD / RAW / Web 连接地址
- 访问地址通过 `/api/admin/settings` 的 `public_host`、`cups_port`、`web_port` 配置

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
