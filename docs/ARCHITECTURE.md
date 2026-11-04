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
│        │ REST + SSE                      │
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
- 管理打印任务状态机
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
- 手机优先、PWA、深色模式、中英文

### 2.3 CUPS

- 容器内单独运行一个 `cupsd`
- 监听 `8631`
- 两个队列指向同一物理设备
- 使用 `brlaser` PPD
- PPD/RAW 队列的 URI 相同，但队列类型不同
- USB backend 加 `flock`，跨队列串行化

## 3. 打印数据流

### 3.1 网页 PPD 打印

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
IPP Print-Job → CUPS RAW 队列
  ▼
USB backend（不经过过滤器）
  ▼
打印机
```

### 3.3 大文件策略

- 所有网页输入统一转 PDF
- 避免 `gstoraster` 对 PostScript 的 bbox 慢扫描
- 转换在独立子进程中执行，设置超时/内存限制
- 超时或失败返回明确错误，不占用打印队列

## 4. 打印机与队列模型

P1 只做单打印机，但数据结构支持多台：

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
  - IPP Everywhere → `-m everywhere`
  - Brother USB → brlaser PPD
  - 其他 → `lpinfo -m` 推荐

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
