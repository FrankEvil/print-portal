# 当前环境记录

> 这是迁移前的现场记录，供新工作区开发时参考；新项目不要依赖这些临时路径/端口。

## NAS

- 系统：fnOS / Debian 12（bookworm），x86_64
- 主机名：`fnos`
- 局域网 IP：`192.168.2.30`
- Mac 客户端：`192.168.2.123`
- 打印机：Brother DCP-7060D，USB
  - `usb://Brother/DCP-7060D?serial=E69807B3J831059`
  - `lsusb`：`04f9:0249`

## 临时/现有 CUPS（迁移前）

### 8631：cups-web 主 CUPS

- 应用：cups-web（第三方闭源）
- PPD 队列：`Brother-7060d`
- PPD：brlaser v6
- 网页打印使用此队列
- 历史问题：cups-web 的 ListPrinters 在两个队列共用同一 DeviceURI 时，会把主队列 URI 映射到 raw

### 8632：独立 raw CUPS（临时）

- systemd 服务：`cups-raw.service`
- raw 队列：`Brother-7060d-raw`
- 只给 Mac 本地 Brother 驱动使用
- 只监听 `192.168.2.30`/`127.0.0.1`
- 当前 Mac 队列指向 8632

## 目标架构

新项目不再依赖 8631/8632 的临时布局：

- 一个容器内置 CUPS
- 一个 CUPS 实例，两个队列（PPD + RAW）
- Web 只走 PPD
- 本地可走 PPD/RAW
- 端口：Web 8088，CUPS 8631

## 已知问题/经验

- 大 PostScript 走 `gstoraster` 的 bbox 扫描很慢；Web 应统一转 PDF
- raw 队列不要暴露公网
- 两个队列共用 USB 时，需要在 USB backend 层加 flock 串行化
- 管理页/打印机配置必须动态化，不要硬编码 IP/队列名
