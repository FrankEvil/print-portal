# Print Portal 路线图

## P0：脚手架与最小验证

- [x] Go 服务 `/healthz`
- [x] Vue 3 页面骨架
- [x] SQLite 连接与迁移
- [x] CUPS 命令执行封装
- [x] Docker 内 CUPS 启动配置
- [x] 单打印机 PPD 队列打印测试（模拟 CUPS 验证）

## P1：目标版本

### 打印

- [x] 响应式打印页：手机 + 电脑 Chrome 同一套页面
- [x] 单文件上传 PDF/图片/Office
- [x] 页码、双面、份数、纸张、方向
- [x] 异步任务队列：提交立即返回，后台转换/提交，前端轮询状态
- [x] 任务状态、取消、重试
- [x] 大文件转 PDF，避免 PostScript bbox 慢路径
- [x] 历史任务

### 打印机管理

- [x] 打印机配置页（P1 支持多台）
- [x] PPD/RAW 双队列配置
- [x] `device-uri` 修改同步两个队列
- [x] PPD 模型选择/上传
- [x] 测试打印
- [x] 暂停/恢复/接受/拒绝/清空

### CUPS 控制台

- [x] CUPS 版本、状态、队列、任务展示
- [x] 常用参数表单
- [x] PPD 默认选项
- [x] 高级 cupsd.conf 编辑器（校验/备份/回滚）
- [x] 日志查看

### 设备发现/扫描

- [x] USB/IPP/mDNS 打印机发现（CUPS lpinfo）
- [x] 自动匹配驱动并创建 PPD+RAW 队列
- [x] 扫描仪发现（`scanimage -L`，P1 收尾项）
- [x] 扫描 PDF/JPEG/PNG（代码完成；USB 扫描依赖 brscan4，容器内驱动验证待做）
- [x] 扫描预览/下载

### 驱动管理

- [x] 驱动列表与状态检测（ready/degraded/missing）
- [x] 型号索引搜索（lpinfo -m + foomatic）
- [x] 上传安装/卸载驱动包与 PPD
- [x] 自动匹配（IPP Everywhere / USB 1284DeviceID）

### 系统与部署

- [x] 一体化 CUPS 容器（Dockerfile / compose 已提供；本机 Docker Hub 拉取超时，未完成镜像构建验证）
- [x] 端口 8088 / 8631
- [x] USB 挂载
- [x] 配置持久化
- [x] 日志页（P1）
- [ ] 备份/恢复（P1.1）
- [x] 单用户 `ADMIN_TOKEN`
- [x] 局域网访问控制
- [x] 深色模式（P1）、中文（P1）、英文（后续）

## P2：以后

- 用户体系、权限、配额、审批、审计
- 多打印机/多租户
- 扫描到邮件/网盘
- 邮件打印
- 公网/VPN 集成
- 插件系统
