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


## 4.1 普通版使用预构建镜像

普通版不需要从源码构建，直接使用 GitHub Container Registry 镜像：

```bash
cp .env.example .env
# 编辑 .env：
#   ADMIN_TOKEN=你的访问令牌
#   PRINT_PORTAL_PUBLIC_HOST=192.168.2.30

docker compose -f docker-compose.standalone.yml up -d
```

首次启动会拉取：

```text
ghcr.io/frankevil/print-portal:0.3.6
```

更新：

```bash
docker compose -f docker-compose.standalone.yml pull
docker compose -f docker-compose.standalone.yml up -d
```

普通版特点：

- Token 登录
- 显示「访问地址」配置
- 支持 `PRINT_PORTAL_PUBLIC_HOST`
- 不依赖 fnOS 统一网关

## 4.2 快速验证 / 热更新

只改 Go 后端时，不需要重新构建完整镜像：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64   go build -trimpath -ldflags='-s -w'   -o printd-linux-arm64 ./cmd/printd

docker compose -f docker-compose.local.yml build
docker compose -f docker-compose.local.yml up -d
```

只改前端时：

```bash
cd web && pnpm build && cd ..
docker compose -f docker-compose.local.yml build
docker compose -f docker-compose.local.yml up -d
```

fnOS 设备上临时验证可以直接替换容器内文件：

```bash
# Go 二进制
docker cp /tmp/printd-linux-amd64 print-portal:/usr/local/bin/printd
docker restart print-portal

# 前端静态文件
docker cp /tmp/print-portal-dist.tgz print-portal:/tmp/dist.tgz
docker exec print-portal sh -c 'mkdir -p /opt/print-portal/web && tar -xzf /tmp/dist.tgz -C /opt/print-portal/web'
```

热更新的改动只用于测试；正式更新仍然需要重新构建镜像和 fpk。

## 4.3 fnOS 版与普通版差异

| 功能 | fnOS 版 | 普通版 |
|---|---|---|
| 登录 | 飞牛统一网关 | Print Portal Token |
| 入口 | `/app/<appname>` | `http://NAS:8088/` |
| 访问地址配置 | 隐藏 | 显示 |
| Token 登录页 | 不显示 | 显示 |
| IPP 地址 | 自动生成 | 自动/可配置 |
| 更新 | fpk 安装 | Docker 镜像更新 |

两个版本共用同一套业务代码，通过 `PRINT_PORTAL_AUTH_MODE` 区分。

## 4.4 AirPrint / Mopria

AirPrint 需要 `network_mode: host`，并且局域网允许 UDP 5353 组播。详细实现和 Android/Mopria 连接方式见：

- [AIRPRINT.md](AIRPRINT.md)

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
- `PRINTD_ADDR: "127.0.0.1:8088"`：Web 仅监听本机，局域网只能经 `web-proxy` 或飞牛网关访问
- `PRINT_PORTAL_IPP_ADDR: "0.0.0.0:8631"`：IPP 网关按需限制到局域网网段
- `web-proxy` sidecar：host 网络监听 `8080`，反向代理到 `127.0.0.1:8088`

fnOS 应用包使用飞牛统一网关（Unix Socket）鉴权，`PRINTD_ADDR` 同样绑定 `127.0.0.1`；伪造 `X-Trim-*` 请求头无法从局域网直接调用管理 API。

### 5.1 fnOS 域名

`web-proxy` 是一个 nginx sidecar：

```text
fnOS 域名 → print-portal-web-proxy:8080
             → 127.0.0.1:8088
```

在 fnOS 中把域名反代到 `print-portal-web-proxy` 的 `8080`。

### 5.2 fnOS 防火墙

建议在 fnOS 防火墙中：

- 允许 `192.168.2.0/24` 访问 TCP 8631
- 拒绝其他来源访问 8631
- 公网只开放 Web 入口（fnOS 版本为 8080）

不要把 8631 暴露到公网。

## 6. 端口

| 端口 | 用途 | 建议访问范围 |
|---|---|---|
| 8088 | Web 打印 / 管理 | 仅本机（fnOS 版本） |
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
