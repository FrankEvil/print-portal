# Print Portal for fnOS

单用户局域网打印门户。基于 Docker 运行，包含 CUPS、brlaser、LibreOffice、Ghostscript 等运行时。

安装前需要将镜像发布到可访问的镜像仓库，并替换：

```text
deployments/fnos-app/print-portal/fnos/docker/docker-compose.yaml
```

中的 `ghcr.io/your-github-account/print-portal`。
