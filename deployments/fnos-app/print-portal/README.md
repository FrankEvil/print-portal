# Print Portal fnOS 包

将本目录复制到 [conversun/fnos-apps](https://github.com/conversun/fnos-apps) 的 `apps/print-portal`，将 `deployments/fnos-app/scripts/apps/print-portal` 复制到 `scripts/apps/print-portal`，即可使用该仓库的打包流程。

## 构建

```bash
cd fnos-apps
VERSION=0.1.0 bash scripts/apps/print-portal/build.sh
bash scripts/build-fpk.sh apps/print-portal app.tgz 0.1.0 x86
```

生成：

```text
print-portal_0.1.0_x86.fpk
```

## 注意

- `docker-compose.yaml` 中的镜像地址目前是占位符
- 先构建并推送 `print-portal:0.1.0` 到 GHCR / Docker Hub
- fnOS 应用中心通过 `service_port=8088` 打开 Web
- `8631` 是 IPP 端口，应用防火墙规则文件已声明
