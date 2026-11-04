# 新工作区交接说明

## 项目位置

```text
/Users/liuzhaojun/IdeaProjects/print-portal
```

## 当前状态

- 已完成文档：PRD、架构、API、数据模型、路线图、部署、决策记录、当前环境记录
- 已完成 Go 后端脚手架：`cmd/printd` + `internal/*`
- 已完成 Vue 3 + TypeScript 前端脚手架：`web/`
- 已创建 Docker/CUPS 一体化部署草案：`deployments/`
- 已初始化 Git 仓库并提交初始 scaffold

## 本机环境

- Node.js：`v22.18.0`
- pnpm：`10.15.1`
- Go：**当前未安装**（`go: command not found`），开发前需要安装 Go 1.23+
- Docker：部署时需要
- 当前 NAS 的临时 CUPS 布局见 `docs/CURRENT_ENV.md`

## 建议下一步

1. 安装 Go 1.23+ 和 IntelliJ Go 插件
2. `cd web && pnpm install`，确认 `pnpm build` 通过
3. 实现 SQLite 初始化和 `printers`/`jobs`/`settings` 表
4. 实现 CUPS 适配层：
   - `lpstat`/`lp`/`lpadmin` 命令封装
   - 单打印机 PPD + RAW 队列配置
5. 实现 `GET /healthz`、`GET /api/printers`、`POST /api/print`
6. 实现管理页的打印机配置（PPD/RAW、设备地址、测试打印）
7. 接入 P1 的设备发现和扫描
8. 最后完成一体化 CUPS Docker 镜像

## 关键约束

- 单用户：不做 users/login/JWT/OIDC
- 管理页只用一个 `ADMIN_TOKEN`
- 网页只走 PPD
- 本地客户端可连 PPD 或 RAW
- 打印机配置必须页面化，不硬编码 IP/队列
- CUPS 配置修改要校验、备份、可回滚
- 项目要能长期分享，许可证尚未决定（见 `docs/DECISIONS.md`）
