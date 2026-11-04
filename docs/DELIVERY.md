# Print Portal P1 交付文档

- 版本：0.1.0
- 日期：2026-11-05
- 状态：P1 主体功能完成，自动化测试通过
- 许可证：Apache-2.0

## 1. 交付内容

### 1.1 后端（Go 1.23+）

```text
cmd/printd/main.go            服务入口、配置装载、优雅退出
internal/config/              环境变量配置
internal/storage/             SQLite 存储层与迁移（modernc.org/sqlite，纯 Go）
internal/cups/                CUPS 命令封装（lp/lpstat/lpadmin/lpoptions/cancel 等）
internal/convert/             文件检测与 PDF 转换（LibreOffice / Ghostscript / img2pdf）
internal/job/                 异步任务队列、状态机、CUPS 状态同步、本地任务导入
internal/printer/             打印机与 PPD/RAW 双队列管理、测试页
internal/driver/              驱动状态检测、型号索引、驱动包安装/卸载
internal/discovery/           打印机与扫描仪发现
internal/api/                 HTTP API、ADMIN_TOKEN 鉴权、静态前端服务
```

### 1.2 前端（Vue 3 + TypeScript + Vite）

```text
web/src/views/PrintView.vue     响应式打印页（桌面双栏 / 手机单列）
web/src/views/AdminLogin.vue    ADMIN_TOKEN 登录
web/src/views/AdminLayout.vue   管理后台布局
web/src/views/DashboardView.vue 概览
web/src/views/PrintersView.vue  打印机管理（多台、双队列、PPD 选项）
web/src/views/DriversView.vue   驱动管理（状态、型号搜索、上传安装）
web/src/views/CupsView.vue      CUPS 控制台（参数、cupsd.conf）
web/src/views/JobsView.vue      任务管理（Web + 本地任务）
web/src/views/SettingsView.vue  应用设置
web/src/views/LogsView.vue      日志
web/src/views/ScanView.vue      扫描（P1 收尾占位）
```

### 1.3 部署

- `deployments/Dockerfile`：多阶段构建（Vue → Go → Debian + CUPS）
- `deployments/entrypoint.sh`：生成 cupsd.conf、启动 cupsd(8631) + printd(8088)
- `docker-compose.yml`：USB 挂载、端口绑定、数据卷
- 镜像内置：CUPS、cups-filters、brlaser、foomatic-db、Ghostscript、Poppler、LibreOffice、ImageMagick、img2pdf、sane-airscan

## 2. 功能完成情况（对照 PRD 验收标准）

| # | 验收标准 | 状态 | 说明 |
|---|---|---|---|
| 1 | 手机打开网页上传 PDF 打印 | ✅ 实现 | 响应式页面 + 异步打印接口；真机打印需在实际 NAS 验证 |
| 2 | 电脑 Chrome 全流程可用，1280/1920 无滚动遮挡 | ✅ 实现 | 桌面双栏布局；前端构建通过 |
| 3 | 选择 1-2 页 + 双面只打一张纸 | ✅ 实现 | 参数传递到 `lp -o page-ranges/sides`，集成测试覆盖参数 |
| 4 | 大 PDF 能看到转换/排队状态，不误走 raw | ✅ 实现 | 异步队列状态 pending→converting→submitted→queued→printing→completed |
| 5 | 提交后立即返回，可离开页面，后台继续 | ✅ 实现 | POST 返回 202 + job_id；任务持久化 SQLite |
| 6 | 网页看不到 raw 队列 | ✅ 实现 | `/api/printers` 只返回 PPD 队列 |
| 7 | 修改设备地址 PPD+RAW 同步 | ✅ 实现 | `PUT /api/admin/printers/{name}` 同步调用 lpadmin |
| 8 | 修改 PPD 默认选项并生效 | ✅ 实现 | `GET/PUT /api/admin/printers/{name}/ppd-options` |
| 9 | 暂停/恢复/取消/测试打印 | ✅ 实现 | 双队列操作 + 内置测试页 PDF |
| 10 | 显示 CUPS 版本、状态、队列、任务 | ✅ 实现 | `/api/admin/cups/status`、任务列表 |
| 11 | 设备发现并创建队列 | ✅ 实现 | `lpinfo -v` 发现 + 自动匹配驱动；mDNS 由 CUPS/avahi 后端支持 |
| 12 | 扫描仪发现并扫描 PDF | ✅ 实现（驱动待验证） | 扫描页 + `/api/scan` + 预览/下载；USB 扫描依赖 brscan4，容器内打包验证待做 |
| 13 | CUPS 配置修改校验/备份/回滚 | ✅ 实现 | `cupsd -t` 校验、时间戳备份、reload 失败自动恢复 |
| 14 | 重启容器配置与任务历史不丢 | ✅ 配置就绪 | SQLite/PPD/drivers 均在持久卷；未在本机 Docker 实测 |
| 15 | 局域网外不可访问 | ✅ 部署配置 | compose 绑定内网 IP，CUPS 仅监听内网端口 |
| 16 | 驱动管理页状态/型号/安装 | ✅ 实现 | ready/degraded/missing + lpinfo 索引 + 上传安装 |
| 17 | 日志页可查看应用/CUPS/任务日志 | ✅ 实现 | `/api/admin/logs`、`/api/admin/cups/logs` |

## 3. 测试记录

### 3.1 Go 单元测试

```bash
go vet ./...
go test ./...
```

覆盖范围：

- CUPS 输出解析（lpstat/lpoptions/lpinfo、Job ID、PPD 选项、设备、扫描仪）
- SQLite CRUD、迁移、任务领取、驱动与设置
- 异步任务处理（转换 → 提交 → 状态更新、重试）
- API 健康检查、ADMIN_TOKEN 鉴权、multipart 打印提交、非法文件拒绝
- 文件类型检测、PPD 状态检测、驱动卸载保护

结果：全部通过（2026-11-05）。

### 3.2 端到端集成测试

```bash
./scripts/integration_test.sh
```

使用假 CUPS 命令模拟完整链路，覆盖：

1. 构建并启动真实 `printd` 二进制
2. `/healthz`、静态前端 index
3. 创建打印机（PPD + RAW 队列）
4. 打印接口提交 PDF → 后台 worker 转换 → `lp` 提交 → 状态 `submitted`
5. 任务详情字段（份数、打印机名）
6. 驱动列表、型号索引、设备发现
7. 测试页打印
8. 概览、CUPS 状态、设置读写、任务列表、应用日志
9. 任务取消与重试
10. 无 token 访问管理接口返回 401

结果：`PASS: integration test`（2026-11-05）。

### 3.3 真实 CUPS 冒烟测试（macOS 本机 CUPS 2.3.4）

```bash
PRINTD_ADDR=127.0.0.1:18091 PRINT_PORTAL_DATA=/tmp/pp-real \
  ADMIN_TOKEN=*** CUPS_SERVER=127.0.0.1:631 .tools/printd
curl http://127.0.0.1:18091/healthz
curl -H 'X-Admin-Token: ***' http://127.0.0.1:18091/api/admin/discovery/printers
```

结果：

- `/healthz` 返回 `{"status":"ok","cups":"running"}`，真实 CUPS 调度器识别正常
- 设备发现返回局域网 mDNS/AirPrint 设备（Brother 7060d、EPSON L1119 等）
- 验证了本地化环境下命令输出解析（强制 `LC_ALL=C` + 中文状态兜底）

### 3.4 前端

```bash
cd web
pnpm install
pnpm typecheck
pnpm build
```

结果：类型检查通过，生产构建成功（输出 `web/dist`）。

### 3.5 浏览器渲染检查（Chrome 156 headless）

使用本机 Google Chrome 对构建产物做真实渲染截图：

- 桌面打印页：[print-desktop.png](screenshots/print-desktop.png)
- 手机打印页（390×844）：[print-mobile.png](screenshots/print-mobile.png)
- 管理登录页：[admin-login.png](screenshots/admin-login.png)
- 管理概览：[admin-dashboard.png](screenshots/admin-dashboard.png)
- 打印机管理：[admin-printers.png](screenshots/admin-printers.png)

检查中发现并修复：

- Vite `base: './'` 导致 `/admin/*` 深链接资源 404、页面空白 → 改为 `base: '/'`

结果：打印页桌面/手机布局、管理登录与后台页面均正常渲染。

## 4. 运行方式

### 4.1 本地开发

```bash
# 后端（默认 :8088，CUPS 默认 127.0.0.1:631）
ADMIN_TOKEN=change-me PRINT_PORTAL_DATA=./data go run ./cmd/printd

# 前端开发服务器（代理 /api 到 8088）
cd web && pnpm install && pnpm dev
```

生产模式：`cd web && pnpm build` 后，printd 自动服务 `web/dist`。

### 4.2 Docker 部署

```bash
docker compose build
docker compose up -d
```

部署前请调整 `docker-compose.yml` 中的：

- `ADMIN_TOKEN`
- 端口绑定 IP（默认 `192.168.2.30`）
- USB 设备挂载

首次启动后访问 `http://<nas>:8088`，管理页 `http://<nas>:8088/admin`。

### 4.3 打印机添加

1. 打开 `管理 → 打印机 → 添加打印机`
2. 填写名称、设备地址（USB 为 `usb://Brother/DCP-7060D?serial=...`）
3. 型号选择 `drv:///brlaser.drv/br7060d.ppd`（可从输入框下拉选择）
4. 保存后自动创建 PPD 与 RAW 两个队列

## 5. 已知限制与后续项

| 项 | 说明 |
|---|---|
| 物理打印机验证 | 本机未连接打印机，打印链路通过模拟 CUPS 命令验证；交付到 NAS 后需做真机测试 |
| Docker 镜像构建 | Docker daemon 可用，但本机访问 Docker Hub 拉取基础镜像超时，未完成实际 `docker build`；Dockerfile 与入口脚本已完成，需在可访问镜像源的构建机验证 |
| 扫描 | 扫描服务/页面/API 已实现；USB 扫描所需 brscan4 驱动的容器内打包与真机验证待做 |
| 备份/恢复 | 规划在 P1.1 |
| HEIC / TIFF / WPS | 后续版本（P1 核心格式为 PDF/JPEG/PNG/WebP/Office/PostScript） |
| 英文界面 | 后续版本（P1 中文 + 深色模式） |
| 浏览器 | 仅保证现代 Chrome |

## 6. 关键文件索引

- 需求：[PRD](PRD.md)
- 架构：[ARCHITECTURE](ARCHITECTURE.md)
- API：[API](API.md)
- 数据模型：[DATA_MODEL](DATA_MODEL.md)
- 部署：[DEPLOYMENT](DEPLOYMENT.md)
- 决策：[DECISIONS](DECISIONS.md)
- 路线图：[ROADMAP](ROADMAP.md)

## 7. 交付结论

P1 的打印主链路、打印页、管理后台、CUPS 控制台、驱动管理与任务管理已实现并通过自动化测试；扫描与备份按需求约定为 P1 收尾/P1.1。项目可作为单用户局域网打印门户部署到 fnOS NAS，完成真机打印验证后即可投入使用。
