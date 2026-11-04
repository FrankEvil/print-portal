# 数据库迁移

P1 使用 SQLite。迁移文件按顺序执行，例如 `0001_init.sql`。

建议在 Go 中使用 `goose` 或 `golang-migrate`。单进程启动时自动迁移；多实例部署时改为独立迁移命令。
