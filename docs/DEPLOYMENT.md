# 部署说明

本文说明如何把 Print Portal 部署到一台局域网主机（NAS / 小主机 / Docker 服务器）。

## 1. 运行条件

- Linux x86_64 或 ARM64 主机
- Docker 24+
- Docker Compose v2
- USB 打印机（可选；也支持网络打印机）
- 局域网固定 IP

容器包含：

- CUPS 2.4
- 打印驱动（brlaser、cups-filters、foomatic 索引）
- LibreOffice / Ghostscript / Poppler / ImageMagick
- Go 服务 + Vue 静态前端
- sane-airscan（扫描，可选）

宿主机不需要单独安装 CUPS 或 LibreOffice。

## 2. 目录结构

```text
cmd/printd/                 Go 服务入口
internal/                   Go 后端
web/                        Vue 前端
deployments/
  Dockerfile                完整多阶段构建
  Dockerfile.fnos           fnOS 预构建镜像（快）
  Dockerfile.local          本地开发快速镜像
  entrypoint.sh             CUPS + printd 启动入口
docker-compose.yml          通用构建
docker-compose.fnos.yml     fnOS 部署
docker-compose.local.yml    本地开发
```

## 3. 配置

环境变量：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `ADMIN_TOKEN` | 无 | 管理后台和 `/api/admin/*` 的访问 Token，必须设置 |
| `PRINTD_ADDR` | `:8088` | Web 监听地址 |
| `CUPS_SERVER` | `127.0.0.1:631` | 容器内 CUPS 地址 |
| `PRINT_PORTAL_IPP_ADDR` | `0.0.0.0:8631` | IPP 网关监听地址；生产建议绑定局域网 IP |
| `MAX_UPLOAD_MB` | `200` | 单文件上传大小上限（也可在设置页调整） |

管理 Token 必须是足够长的随机字符串，不要使用默认值。

## 4. 通用 Docker 部署

推荐使用预构建镜像的独立版 compose：

```bash
cp .env.example .env
# 编辑 .env，至少设置 ADMIN_TOKEN 和 PRINT_PORTAL_PUBLIC_HOST
docker compose -f docker-compose.standalone.yml up -d
```

也可以从源码构建：

```bash
cp .env.example .env
# 编辑 .env，至少设置 ADMIN_TOKEN
docker compose up -d --build
```

访问：

```text
http://<主机 IP>:8088/
```

如果使用 USB 打印机，需要确认 compose 中挂载了 `/dev/bus/usb` 并有 `privileged: true`。

## 5. fnOS 部署

fnOS 推荐使用预构建方式。先在构建机生成 Linux amd64 二进制和前端：

```bash
# 在项目根目录
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath -ldflags='-s -w' -o printd-linux-amd64 ./cmd/printd

cd web && pnpm install && pnpm build
```

然后把 `printd-linux-amd64`、`web/dist` 和 `deployments/` 一起放到 fnOS 项目目录：

```bash
cd /vol2/1000/docker-images/print-portal
docker compose -f docker-compose.fnos.yml up -d --build
```

当前 `docker-compose.fnos.yml` 使用：

- `network_mode: host`：保留 mDNS / USB / 局域网发现
- `PRINT_PORTAL_IPP_ADDR: "192.168.2.30:8631"`：只监听局域网 IP
- `web-proxy` sidecar：把 `8080` 暴露给 fnOS 域名反代

### 5.1 fnOS 域名

`web-proxy` 是一个 nginx sidecar：

```text
fnOS 域名 → print-portal-web-proxy:8080
             → host.docker.internal:8088
```

在 fnOS 中把域名反代到 `print-portal-web-proxy` 的 `8080`。

### 5.2 fnOS 防火墙

建议在 fnOS 防火墙中：

- 允许 `192.168.2.0/24` 访问 TCP 8631
- 拒绝其他来源访问 8631
- 公网只开放 Web 入口（8080 或 8088）

不要把 8631 暴露到公网。

## 6. 端口

| 端口 | 用途 | 建议访问范围 |
|---|---|---|
| 8088 | Web 打印 / 管理 | 局域网 |
| 8631 | printd IPP 网关 | 仅局域网 |
| 8080 | fnOS Web 代理（可选） | 按域名反代配置 |
| 631 | 容器内 CUPS | 仅 127.0.0.1 |

## 7. 数据持久化

必须持久化：

```text
/app/data           SQLite、上传文件、任务、驱动
/etc/cups           打印机 / PPD / cupsd.conf
/var/spool/cups     打印队列
/var/log/cups       CUPS 日志
```

`docker-compose.fnos.yml` 使用宿主机目录；通用 compose 使用 named volumes。

## 8. 启动流程

`entrypoint.sh`：

1. 初始化 `/etc/cups` 配置
2. 启动 `cupsd`（仅 127.0.0.1:631）
3. 等待 CUPS ready
4. 启动 `printd`（Web + IPP 网关）
5. 退出时停止子进程

CUPS 创建的任务文件权限由 `inotify` 监听修正，避免 `lp` 过滤器无法读取任务文件。

## 9. 更新

```bash
# 备份
tar -czf print-portal-backup.tgz data cups-etc cups-spool cups-log

# 更新镜像
docker compose -f docker-compose.fnos.yml build
docker compose -f docker-compose.fnos.yml up -d
```

## 10. 升级 / 回滚

- 代码更新前保留 `data/`、`cups-etc/` 备份
- 若新版本异常，停止容器并替换回旧二进制 / 镜像后启动
- 数据库迁移是向前兼容的；不要用旧版本打开已升级的数据库

## 11. 故障排查

```bash
docker compose ps
docker compose logs -f print-portal
docker exec print-portal lpstat -p
docker exec print-portal lpstat -o
curl http://127.0.0.1:8088/healthz
```

打印机长时间显示 `Sending data to printer` 时，先检查打印机面板是否缺纸、卡纸或 USB 连接异常。
