# 部署文件说明

| 文件 | 用途 |
|---|---|
| `Dockerfile` | 完整多阶段构建：Vue → Go → Debian + CUPS |
| `Dockerfile.fnos` | fnOS 预构建镜像：复制 `printd-linux-amd64` 和 `web/dist` |
| `Dockerfile.local` | 本地开发快速镜像：在已有运行时镜像上只替换二进制和前端 |
| `entrypoint.sh` | 初始化 CUPS、启动 cupsd 和 printd |
| `nginx-proxy.conf` | fnOS Web 域名反代 sidecar 配置 |

完整部署、端口、数据目录和安全说明见 [../docs/DEPLOYMENT.md](../docs/DEPLOYMENT.md)。
