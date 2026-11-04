# Print Portal（打印门户）

面向家庭/小办公室的 **单用户、局域网 Web 打印门户**。内置 CUPS，网页上传即打印；本地客户端可自由选择 PPD 或 RAW。

## 目标

- 手机/电脑打开网页即可打印，无需安装驱动
- 管理页配置打印机和 CUPS 参数，不写死 IP/队列
- 网页只走 PPD，本地客户端可选择 PPD 或 RAW
- 单用户、无用户体系；管理页用 `ADMIN_TOKEN` 保护
- Go + Vue 3 + SQLite + 一体化 CUPS 容器

## 文档

- [产品需求（PRD）](docs/PRD.md)
- [架构设计](docs/ARCHITECTURE.md)
- [API 设计](docs/API.md)
- [数据模型](docs/DATA_MODEL.md)
- [路线图](docs/ROADMAP.md)
- [部署说明](docs/DEPLOYMENT.md)
- [技术决策记录](docs/DECISIONS.md)
- [新工作区交接](docs/HANDOFF.md)

## 当前状态

P1 主体功能已实现：响应式打印页、异步任务队列、打印机管理（PPD + RAW 双队列）、驱动管理、CUPS 控制台、任务管理、设置与日志。

- 扫描：P1 收尾项（USB 扫描依赖 brscan4，未通过则顺延 P1.1）
- 备份/恢复：P1.1
- 详细交付说明与测试记录见 [docs/DELIVERY.md](docs/DELIVERY.md)

## 目录

```text
cmd/printd/          Go 服务入口
internal/            Go 后端模块
web/                 Vue 3 + TypeScript 前端
deployments/         Docker / CUPS 集成部署
docs/                产品、架构、API 文档
```

## 开发环境

- Go 1.23+
- Node.js 22+
- pnpm 10+
- Docker（部署 CUPS 容器时需要）

```bash
# 后端（需要 Go 1.23+）
go run ./cmd/printd

# 前端
cd web
pnpm install
pnpm dev

# 测试
go test ./...
./scripts/integration_test.sh
cd web && pnpm typecheck && pnpm build
```

## License

[Apache License 2.0](LICENSE)
