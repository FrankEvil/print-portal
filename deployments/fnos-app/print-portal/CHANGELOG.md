# Changelog

## 0.2.7

- 修复 fnOS 桌面图标不显示：图标改用 images/icon_{0}.png 约定

## 0.2.6

- 修复 fnOS 桌面图标不显示（manifest 格式与 appname 对齐）

## 0.2.5

- 修复暂停任务被 CUPS 状态同步覆盖
- PPD 权限修复
- AirPrint 稳定性与任务状态完善

## 0.2.4

- 修复任务页面暂停/恢复按钮同时显示
- 增加任务“已暂停”状态

## 0.2.3

- 修复 App Center 安装后 CUPS PPD 权限导致任务排队失败

## 0.2.2

- 完善内置 mDNS：网卡/IP 变化、CUPS 状态联动、30 秒抖动缓冲

## 0.2.1

- 移除容器内 Avahi/D-Bus，仅使用内置 Go mDNS

## 0.2.0

- 内置 AirPrint / Mopria mDNS 广播（每台打印机独立开关）
- 无宿主 Avahi / D-Bus 依赖
- 改进移动端管理页面
- 共享页 AirPrint 状态标签与地址复制图标
- 任务管理导入最近 AirPrint / IPP 任务

## 0.1.9

- 接入 fnOS 原生文件选择器，支持直接打印 NAS 文件

## 0.1.8

- 增加 Epson ESC/P-R 驱动（EPSON ET-1110 等）

## 0.1.7

- fnOS 模式隐藏访问地址配置，由统一网关管理

## 0.1.6

- 共享页自动检测局域网 IP，支持 Docker 公网主机配置

## 0.1.5

- 共享页自动检测 fnOS 局域网 IP

## 0.1.4

- 使用当前 iframe 地址在新窗口打开；移除固定 fnOS 端口配置

## 0.1.3

- 修复 fnOS 5666 端口跳转与网关 API 路由

## 0.1.2

- 修复 fnOS 网关下 API/SPA 路由前缀

## 0.1.1

- 修复 fnOS 网关静态资源路径

## 0.1.0

- 首次发布
