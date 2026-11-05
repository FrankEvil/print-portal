# 待办与需求记录

## 局域网 IPP 分块代理（待实现）

**状态**：已记录，待排期

**背景**：网页打印已经由应用层完成「转 PDF → 按页拆分 → 逐块提交」，但局域网客户端直接用 PPD 队列打印时仍可能一次性提交大任务，导致 CUPS PPD 过滤器/USB 缓冲过载。

**目标**：在 `printd` 的 `8631` IPP 入口增加 IPP-aware 代理：

1. 接收局域网客户端的 IPP 打印任务
2. 将文档落盘并识别格式（PDF/PostScript 优先）
3. 按 `chunk_pages` 拆成小块
4. 逐块提交到内部 CUPS PPD 队列，等待上一块完成后提交下一块
5. 对客户端暴露一个代理任务，映射子任务状态、支持取消
6. 保留 `allow_access` 访问校验

**待确认/限制**：

- 客户端发送 PDF/PostScript 时可拆分；如果发送 CUPS raster/PCL，需要评估是否原样转发或拒绝
- 需要支持 `Print-Job` 及 `Create-Job`/`Send-Document`（Windows 客户端）
- 需要 macOS / Windows IPP 客户端兼容测试
- 不能消除 PPD 本身渲染慢的问题，只解决大任务一次性灌满 USB 的问题

## 设备维护与厂商驱动工具（讨论中）

**状态**：待讨论

**背景**：本地安装厂商驱动时，驱动软件通常还提供状态、耗材、清洗/校准、固件、扫描等维护工具；当前 Web/CUPS 方式主要提供打印队列与 PPD 选项。

**待确认能力范围**：

- 耗材/状态查询（CUPS/IPP marker attributes、SNMP）
- 清洗/校准等维护动作（是否可通过厂商 Linux CLI 触发）
- 厂商驱动设置项（PPD options）
- 固件升级/设备诊断（通常无法通过通用 IPP 实现）
- 是否需要 USB/IP 或本地直连来保留完整厂商工具

**实施方向（待驱动包到位）**：

1. 检查厂商 Linux 驱动包内的 PPD、filter、backend 及维护 CLI
2. 为驱动增加维护适配器（manifest 或代码注册），定义允许的动作：
   - 状态/耗材
   - 清洗/校准
   - 测试页/自检
   - 其他厂商提供的安全 CLI
3. 增加后端接口：
   - `GET /api/admin/printers/{name}/maintenance`：返回驱动能力与当前状态
   - `POST /api/admin/printers/{name}/maintenance/{action}`：执行白名单动作，返回输出
4. 增加「设备维护」页面/入口：
   - 显示驱动名称、能力列表、执行结果
   - 无维护接口时显示说明，只保留 CUPS 状态、PPD 选项、测试页
5. 安全要求：命令白名单、超时、管理员权限、危险操作二次确认

**前置条件**：提供可运行的 Linux 驱动包（`.deb` / `.tar.gz`），并确认真实打印机型号与维护命令。
