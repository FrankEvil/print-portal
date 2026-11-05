# Print Portal 架构设计

## 1. 总览

```text
局域网手机/电脑
        │ HTTP 8088
        ▼
┌──────────────────────────────────────────┐
│ print-portal 容器                        │
│                                          │
│  Vue 3 前端（embed 到 Go 二进制）        │
│        │ REST + 轮询                     │
│        ▼                                 │
│  Go printd 服务                          │
│   ├── 打印 API / 管理 API                │
│   ├── 任务队列 / SQLite                  │
│   ├── 文件转换（LibreOffice/gs）         │
│   ├── 设备发现（IPP/mDNS/USB/SANE）      │
│   └── CUPS 适配层（lp/lpadmin/lpstat）   │
│        │                                 │
│        ▼                                 │
│  CUPS（容器内，8631）                    │
│   ├── PPD 队列：Brother-7060d            │
│   └── RAW 队列：Brother-7060d-raw        │
│        │                                 │
│        ▼                                 │
│  USB backend（flock 串行化）             │
└──────────────────┬───────────────────────┘
                   │ USB
                   ▼
             Brother DCP-7060D
```

## 2. 组件职责

### 2.1 Go `printd`

- 提供 REST API 和静态前端
- 校验/保存配置
- 管理打印任务状态机和异步队列 worker
- 调用转换器
- 调用 CUPS 命令/IPP
- 设备发现、扫描编排
- 日志、健康检查、备份

目录：

```text
cmd/printd/          入口
internal/api/        HTTP 路由
internal/cups/       CUPS 适配
internal/printer/    打印机配置
internal/job/        任务状态机
internal/convert/    文件转换
internal/discovery/  设备/扫描仪发现
internal/storage/    SQLite/文件
internal/config/     配置
```

### 2.2 Vue 3 前端

- `web/` 独立开发，构建后 `embed` 到 Go
- 页面：
  - `/` 打印页
  - `/admin` 管理首页
  - `/admin/printers` 打印机
  - `/admin/discovery` 设备发现
  - `/admin/cups` CUPS 控制台
  - `/admin/jobs` 任务
  - `/admin/settings` 设置
  - `/admin/logs` 日志
  - `/scan` 扫描
- 响应式：手机/平板/桌面 Chrome 共用一套代码，桌面宽屏左右分栏
- 统一设计系统：颜色/字号/间距/圆角/阴影 token 化，组件复用
- 深色模式、中英文；PWA 可选

### 2.3 CUPS

- 容器内单独运行一个 `cupsd`
- 监听 `8631`
- 两个队列指向同一物理设备
- 使用 `brlaser` PPD
- PPD/RAW 队列的 URI 相同，但队列类型不同
- USB backend 加 `flock`，跨队列串行化

### 2.4 驱动管理

- 驱动分三层：内置驱动（镜像只读）、驱动包（上传安装/卸载）、PPD（手动上传）
- `drivers` 表记录驱动包元信息和状态；型号索引从 `lpinfo -m` + foomatic 实时读取，不落库
- 状态检测：解析 PPD 的 `*cupsFilter` / `*cupsFilter2`，检查 `/usr/lib/cups/filter/` 过滤器是否存在 → `ready` / `degraded` / `missing`；保守判定，过滤器与 foomatic 数据任一缺失即 `degraded`
- 驱动持久化：上传的驱动包与 PPD 存 `/app/data/drivers/`，容器启动时重放安装并重新检测
- 卸载保护：打印机正在使用该驱动的 PPD 时拒绝卸载（409）
- 自动匹配：网络 IPP Everywhere → `lpadmin -m everywhere`；USB 读取 1284DeviceID 的 MFG/MDL → 匹配 PPD；匹配不到 → 型号搜索或上传 PPD
- 默认只预装开源可再分发驱动（brlaser、cups-filters），闭源驱动由用户上传

## 3. 打印数据流

### 3.1 网页 PPD 打印（异步队列）

提交阶段只做校验、落盘和入队，立即返回 `202 + job_id`；以下转换与提交在后台 worker 中执行。

```text
浏览器上传
  │
  ▼
printd 接收文件
  │ 类型检测
  ├─ PDF：直接使用
  ├─ 图片：转 PDF
  ├─ Office：LibreOffice 转 PDF
  ├─ PS：ps2pdf 转 PDF
  ▼
调用 CUPS PPD 队列
  lp -d <ppd_queue> \
     -o page-ranges=1-2 \
     -o sides=two-sided-long-edge \
     -n 1 file.pdf
  ▼
CUPS filters: pdftoraster → rastertobrlaser
  ▼
USB backend → 打印机
```

### 3.2 本地 RAW 打印

```text
Mac/Windows 本地驱动
  │ 本地已渲染成打印机原生数据
  ▼
IPP Print-Job → IPP gateway（仅私网/回环，RAW 请求上限 500MB）
  ▼
CUPS RAW 队列
  ▼
USB backend（不经过过滤器）
  ▼
打印机
```

### 3.3 大文件策略

- 所有网页输入统一转 PDF
- 避免 `gstoraster` 对 PostScript 的 bbox 慢扫描
- 多页大文件按 `chunk_pages`（默认 4 页）拆成多个小 PDF，按顺序逐个提交 CUPS；当前块完成后再提交下一块，避免一次性灌满 USB/打印机缓冲
- 每个 chunk 先在应用进程内用 `cupsfilter` + PPD filter 渲染成打印机原生数据，再提交到 RAW 队列；这样既保留 PPD 参数，又避免 CUPS 过滤用户/临时文件权限导致 `pdftopdf` 失败
- 转换在独立子进程中执行，设置超时/内存限制
- 超时或失败返回明确错误，不占用打印队列
- IPP 网关监听在局域网 IP（`PRINT_PORTAL_IPP_ADDR=192.168.2.30:8631`），只接受私网/回环来源；RAW 任务另有 500MB 请求上限

### 3.4 异步任务队列

- `POST /api/print` 只做校验、落盘、写 `jobs` 表（`pending`），返回 `202 Accepted` + `job_id`，不等待转换和 CUPS
- 后台单 worker 串行处理：`pending → converting → submitted → queued → printing → completed`
- 失败进入 `failed` 并记录 `error_message`，支持重试
- 服务重启时把 `converting` 状态的孤儿任务重置为 `pending`
- 前端轮询 `GET /api/jobs/{id}` 获取状态（活动任务 1–2 秒一次）；轮询仅用于界面展示，提交成功即结束本次交互，页面关闭不影响后台任务
- 单打印机、单 worker，任务按创建顺序串行处理，天然串行化 USB 访问；允许连续提交多个任务排队；后续可扩展转换并发

### 3.5 队列分层（应用队列 vs CUPS 队列）

系统有两层队列，职责不同，缺一不可：

| 层 | 归属 | 负责 |
|---|---|---|
| 应用任务队列 | printd 自己维护（SQLite `jobs` + 单 worker） | 接单、格式转换（LibreOffice/Ghostscript）、打印参数持久化、重试、历史记录 |
| CUPS 打印队列 | CUPS（`cupsd`） | 排队调度、过滤器链（pdftoraster 等）、USB backend 输出、打印机状态 |

流程：`POST /api/print` 写入应用队列 → worker 转换 PDF → `lp -d <ppd_queue>` 提交 CUPS → 记录 `cups_job_id` → 轮询 `lpstat` 把 CUPS 状态回写应用任务。

- 执行状态以 CUPS 为准，应用层只做映射与展示
- 取消/暂停需同时作用于两层：先 `cancel` CUPS 任务，再更新应用任务状态
- CUPS 的 `PreserveJobHistory` 只影响 CUPS 侧历史；应用历史以 SQLite 为准
- 本地客户端直连 PPD/RAW 的任务同样存在于 CUPS，管理页任务列表需合并展示（来源标记为「本地」）
- 无法只依赖 CUPS：Office/图片转换、原始参数、重试策略都不是 CUPS 的职责
- 无法只维护应用队列：实际调度、IPP、过滤器链和 USB 输出必须由 CUPS 完成

## 4. 打印机与队列模型

P1 支持多台打印机（数据结构与 UI 均支持多台）：

```text
Printer
  id
  name
  model
  ppd_queue
  raw_queue
  device_uri
  is_default
```

- 一个 Printer 对应一个物理设备
- 同时创建 `ppd_queue` 和 `raw_queue`
- 管理页修改 `device_uri` 时，两个队列同步更新
- 网页只展示 PPD 队列
- 本地可连接 PPD 或 RAW

## 5. CUPS 管理

常用操作：

```bash
# 查看
lpstat -p -v
lpstat -o
lpstat -l -p <queue>
lpoptions -p <queue> -l

# 新增 PPD 队列
lpadmin -p <ppd_queue> -v <device_uri> -m <model> -E

# 新增 RAW 队列
lpadmin -p <raw_queue> -v <device_uri> -E

# 修改地址
lpadmin -p <queue> -v <new_uri>

# 删除
lpadmin -x <queue>

# 队列控制
cupsenable <queue>
cupsdisable <queue>
cupsaccept <queue>
cupsreject <queue>

# 任务
cancel -a <queue>
```

CUPS 系统参数：

- 优先使用 `cupsctl` 修改（`--share-printers`、`--web-interface`、`--debug-logging` 等）
- 高级配置直接编辑 `cupsd.conf`/`cups-files.conf`
- 保存前执行 `cupsd -t`
- 修改前备份，失败回滚

## 6. 设备发现

- USB：`lpinfo -v`
- IPP/AirPrint：`ippfind`、`avahi-browse`
- 扫描仪：`scanimage -L`、`airscan-discover`
- 发现结果缓存到 `discovered_devices`
- 添加时自动选择驱动：
  - IPP Everywhere → `-m everywhere`（免驱动）
  - USB → 读取 1284DeviceID（MFG/MDL），在驱动索引中匹配 PPD
  - 匹配不到 → 型号搜索（`lpinfo -m`）或手动上传 PPD
- 驱动状态检测与安装管理见 2.4

## 7. 单用户与安全

- 无 `users` 表、无登录
- 打印 API 对局域网开放
- 管理 API 使用 `ADMIN_TOKEN`
- 只监听局域网；容器端口绑定 `192.168.2.30`
- 文件上传白名单、大小限制、路径安全
- 管理配置修改需确认
- 不在前端暴露 CUPS 管理密码/证书

## 8. 部署拓扑

```text
NAS 192.168.2.30
├── Docker: print-portal
│   ├── 8088 web
│   ├── 8631 CUPS
│   ├── /dev/bus/usb → 打印机
│   └── volumes: cups-etc/cups-spool/cups-log/app-data
└── 局域网客户端
    ├── 手机 → http://192.168.2.30:8088
    ├── PPD → ipp://192.168.2.30:8631/printers/<ppd>
    └── RAW → ipp://192.168.2.30:8631/printers/<raw>
```

## 9. 关键决策

- 不基于 cups-web：闭源、列表映射 bug、不可长期维护
- 单用户：不做用户系统，避免复杂度
- 自带 CUPS：部署简单，不依赖系统 CUPS
- 一个 CUPS 实例、两个队列：减少进程和 USB 冲突
- 网页只 PPD，本地可 PPD/RAW：兼顾易用性和大文件性能
- Go + Vue：长期维护、低资源、易重构 UI

详见 [DECISIONS.md](DECISIONS.md)。
