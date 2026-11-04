# Print Portal 部署设计

## 1. 目标

一个 Docker 容器包含：

- CUPS 2.4
- brlaser / cups-filters
- Go 服务
- Vue 静态前端
- LibreOffice / Ghostscript / Poppler / sane-airscan

不依赖宿主机单独安装 CUPS。

## 2. 端口

| 端口 | 用途 | 访问范围 |
|---|---|---|
| 8088 | Web 打印/管理 | 局域网 |
| 8631 | CUPS IPP（PPD/RAW） | 局域网 |
| 631 | 不使用 | — |

## 3. USB

```yaml
devices:
  - /dev/bus/usb:/dev/bus/usb
privileged: true
```

CUPS 使用 `usb://Brother/DCP-7060D?serial=...`。

## 4. 目录持久化

```text
/app/data            SQLite、上传临时文件、扫描文件
/etc/cups            打印机/PPD/cupsd.conf
/var/spool/cups      打印队列
/var/log/cups        CUPS 日志
```

## 5. 示例 compose

```yaml
services:
  print-portal:
    build:
      context: ..
      dockerfile: deployments/Dockerfile
    container_name: print-portal
    restart: unless-stopped
    privileged: true
    ports:
      - "192.168.2.30:8088:8088"
      - "192.168.2.30:8631:8631"
    volumes:
      - /dev/bus/usb:/dev/bus/usb
      - print-data:/app/data
      - cups-etc:/etc/cups
      - cups-spool:/var/spool/cups
      - cups-log:/var/log/cups
    environment:
      ADMIN_TOKEN: "change-me"
      CUPS_PORT: "8631"

volumes:
  print-data:
  cups-etc:
  cups-spool:
  cups-log:
```

## 6. 启动顺序

容器入口：

1. 初始化 CUPS 配置目录
2. 启动 `cupsd`（8631）
3. 等待 CUPS socket 就绪
4. 启动 `printd`
5. 退出时优雅停止 cupsd

推荐用 `s6-overlay` 或 `supervisord` 管理两个进程。

## 7. 从现有 cups-web 迁移

1. 停止 cups-web 应用，避免它占用 8631/USB
2. 备份现有 PPD、printers.conf、cupsd.conf
3. 将 brlaser PPD 复制到镜像
4. 启动 print-portal
5. 用页面添加打印机，或导入配置
6. 验证网页 PPD 打印、本地 PPD/RAW 打印
7. 确认无误后卸载 cups-web

## 8. 安全

- 端口绑定 `192.168.2.30`，不暴露公网
- 管理 API 必须 `ADMIN_TOKEN`
- CUPS 仅监听局域网
- 容器以最小权限运行（除 USB 所需）
- 定期备份 `/app/data` 和 `/etc/cups`
