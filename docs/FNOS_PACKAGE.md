# fnOS 应用包 / 上架说明

## 1. 仓库定位

`https://github.com/conversun/fnos-apps` 是一个**第三方 fnOS 应用打包仓库**，使用 `.fpk` 格式发布应用，不是 fnOS 官方应用商店的提交后台。

它的工作方式：

1. 在 `apps/<slug>/fnos/` 定义应用元数据、Docker Compose、UI、权限和向导
2. 在 `scripts/apps/<slug>/` 定义构建合约
3. CI 构建 `.fpk` 并发布 Release
4. 用户下载 `.fpk` 后在 fnOS 应用中心手动安装

## 2. 本项目的 fnOS 包

本仓库已准备：

```text
deployments/fnos-app/
├── print-portal/                 # 复制到 fnos-apps/apps/print-portal
│   ├── fnos/
│   │   ├── manifest
│   │   ├── cmd/main
│   │   ├── cmd/service-setup
│   │   ├── docker/docker-compose.yaml
│   │   ├── config/privilege
│   │   ├── config/resource
│   │   ├── ui/config
│   │   ├── wizard/install
│   │   ├── print-portal.sc
│   │   ├── health.json
│   │   └── ICON*.PNG
│   └── README.md
└── scripts/apps/print-portal/    # 复制到 fnos-apps/scripts/apps/print-portal
```

## 3. 必要的镜像发布

fnOS 的 Docker 应用包**不包含应用镜像本身**，只包含 `docker-compose.yaml`。安装时 fnOS 会从镜像仓库拉取：

```text
ghcr.io/frankevil/print-portal:${VERSION}
```

因此上架前必须：

1. 构建并推送镜像

```bash
# 示例：在项目根目录构建 linux/amd64 镜像
docker buildx build \
  --platform linux/amd64 \
  -f deployments/Dockerfile \
  -t ghcr.io/<你的 GitHub 用户名>/print-portal:0.3.0 \
  --push .
```

2. 把 `deployments/fnos-app/print-portal/fnos/docker/docker-compose.yaml` 中的占位地址替换为实际镜像地址
3. 确认镜像公开可拉取，或在 fnOS Docker 中配置对应仓库认证

如果没有镜像仓库，`.fpk` 只能在本机已有镜像的环境下测试，不能发给别人安装。

## 4. 构建 fpk

把应用定义和构建合约复制到 `fnos-apps`：

```bash
cp -a deployments/fnos-app/print-portal /path/to/fnos-apps/apps/print-portal
cp -a deployments/fnos-app/scripts/apps/print-portal /path/to/fnos-apps/scripts/apps/print-portal
```

然后：

```bash
cd /path/to/fnos-apps

VERSION=0.3.0 bash scripts/apps/print-portal/build.sh
bash scripts/build-fpk.sh apps/print-portal app.tgz 0.3.0 x86
```

生成：

```text
print-portal_0.3.0_x86.fpk
```

## 5. fnOS 应用包注意事项

- `service_port=8088`：应用中心打开 Web 的端口；如与已有应用冲突，可改成其他端口并同步修改 `PRINTD_ADDR`、`ui/config`、`.sc`
- `8631`：IPP 端口，已写入 `.sc`，建议只允许局域网访问
- `network_mode: host`：保留 USB、mDNS、局域网发现能力
- `privileged: true` 和 `/dev/bus/usb`：USB 打印机需要
- `config/resource` 中的 `docker-project` 让 fnOS 管理 Compose 生命周期
- 首次安装向导会要求设置 `ADMIN_TOKEN`
- fnOS 应用包默认以应用用户运行；宿主机 CUPS / USB 权限由容器和 `privileged` 处理

## 6. 当前本地测试包

本仓库 `dist/print-portal_0.3.0_x86.fpk` 是当前测试包，镜像地址为 `ghcr.io/frankevil/print-portal:0.3.0`。

正式上架前请完成：

- [ ] 发布 `print-portal` 镜像
- [ ] 替换 compose 镜像地址
- [ ] 将包复制到 `conversun/fnos-apps` 对应目录
- [ ] 构建 x86 / arm 产物（如需要）
- [ ] 在 fnOS 上实际安装测试
- [ ] 确认 8088 / 8631 端口和防火墙规则

## 7. GitHub Actions 自动构建镜像

仓库已包含：

```text
.github/workflows/docker-publish.yml
```

推送 `main` 或 tag `v0.3.0` 后，会在 GitHub Actions 中构建并推送：

```text
ghcr.io/frankevil/print-portal:latest
ghcr.io/frankevil/print-portal:0.3.0
ghcr.io/frankevil/print-portal:sha-<commit>
```

首次发布后，需要在 GitHub Packages 页面把该镜像设置为 **Public**，否则 fnOS 应用中心无法匿名拉取。
