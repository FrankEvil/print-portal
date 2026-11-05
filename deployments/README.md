# 部署

这是 P1 的一体化容器草案：

- CUPS 2.4
- brlaser / cups-filters
- LibreOffice / Ghostscript / Poppler
- sane-airscan
- Go 服务 + Vue 静态资源

## 构建

```bash
docker compose build
docker compose up -d
```

## 端口

- `8088` Web
- `8631` printd IPP 网关（本地 PPD/RAW，按允许访问开关校验）
- `631` CUPS 内部端口（仅 127.0.0.1）

## 说明

- `entrypoint.sh` 目前是骨架，正式实现时建议用 `s6-overlay` 或 `supervisord` 管理 `cupsd` 和 `printd`
- CUPS 的 `cupsd.conf` / `printers.conf` 初始化、USB 权限、健康检查需要实现阶段补齐
- 不要同时运行 cups-web，避免 8631 和 USB 设备冲突
