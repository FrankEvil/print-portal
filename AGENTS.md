# Print Portal 项目约定

## 项目定位

单用户、局域网、一个物理打印机的 Web 打印门户；内置 CUPS，网页只走 PPD，本地客户端可 PPD 或 RAW。

## 技术栈

- 后端：Go 1.23+（`net/http`，不引入重框架）
- 前端：Vue 3 + TypeScript + Vite + Pinia
- 数据：SQLite
- 打印：CUPS（集成在部署容器内）
- 转换：LibreOffice / Ghostscript / Poppler

## 关键原则

1. 单用户：不做 users/login/JWT/OIDC；管理页仅用一个 `ADMIN_TOKEN`
2. 网页只使用 PPD 队列，不暴露 raw
3. 本地可连接 PPD 或 RAW，两个队列属于同一物理打印机
4. 打印机配置动态读取，不硬编码名称、IP、队列名
5. 所有管理操作优先页面化，CLI 只作为后端实现细节
6. 不把 CUPS 作为外部系统依赖，部署包内自带 CUPS
7. 修改 CUPS 配置必须校验、备份、可回滚

## 文档

- `docs/PRD.md`：P1 目标与验收标准
- `docs/ARCHITECTURE.md`：整体架构
- `docs/API.md`：接口约定
- `docs/DATA_MODEL.md`：数据库表
- `docs/DECISIONS.md`：关键决策

## 当前阶段

先完成 P1：打印页、管理页、CUPS 控制台、设备发现、扫描、任务管理、Docker 一体化部署。
