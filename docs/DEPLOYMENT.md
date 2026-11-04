# Print Portal 部署设计

## 1. 目标

一个 Docker 容器包含：

- CUPS 2.4
- brlaser / cups-filters
- Go 服务
- Vue 静态前端
- LibreOffice / Ghostscript / Poppler / sane-airscan

不依赖宿主机单独安装 CUPS。

## 2. 驱动

- 打印：`printer-driver-brlaser`（Debian 包，开源 GPLv2），官方支持 Brother DCP-7060D
- 型号索引：`foomatic-db` / `foomatic-db-compressed-ppds`（只作索引，不代表驱动可用）
- 过滤器：`cups-filters`
- 扫描：Brother `brscan4`（闭源，P1 收尾项，需从 Brother 官网下载并打包）
- 其他驱动：不预装，P1 驱动管理页支持上传安装第三方驱动包（gutenprint/escpr/hplip/splix/foo2zjs/Canon 等）
- 网络打印机：CUPS 内置 IPP Everywhere（`-m everywhere`），免驱动
- PPD 兜底：管理页支持手动上传 PPD 文件
- 许可证：默认只预装开源可再分发驱动，闭源驱动由用户上传

## 3. 端口

| 端口 | 用途 | 访问范围 |
|---|---|---|
| 8088 | Web 打印/管理 | 局域网 |
| 8631 | CUPS IPP（PPD/RAW） | 局域网 |
| 631 | 不使用 | — |

## 4. USB

```yaml
devices:
  - /dev/bus/usb:/dev/bus/usb
privileged: true
```

CUPS 使用 `usb://Brother/DCP-7060D?serial=...`。

## 5. 目录持久化

```text
/app/data            SQLite、上传临时文件、扫描文件、drivers/（驱动包与 PPD）
/etc/cups            打印机/PPD/cupsd.conf
/var/spool/cups      打印队列
/var/log/cups        CUPS 日志
```

## 6. 示例 compose

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

## 7. 启动顺序

容器入口：

1. 初始化 CUPS 配置目录
2. 启动 `cupsd`（8631）
3. 等待 CUPS socket 就绪
4. 启动 `printd`
5. 退出时优雅停止 cupsd

推荐用 `s6-overlay` 或 `supervisord` 管理两个进程。

## 8. 从现有 cups-web 迁移

1. 停止 cups-web 应用，避免它占用 8631/USB
2. 备份现有 PPD、printers.conf、cupsd.conf
3. 将 brlaser PPD 复制到镜像
4. 启动 print-portal
5. 用页面添加打印机，或导入配置
6. 验证网页 PPD 打印、本地 PPD/RAW 打印
7. 确认无误后卸载 cups-web

## 9. 安全

- 端口绑定 `192.168.2.30`，不暴露公网
- 管理 API 必须 `ADMIN_TOKEN`
- CUPS 仅监听局域网
- 容器内 `cupsd` / `printd` 以 root 运行（CUPS 管理与 `lpadmin` 需要），USB 通过 privileged 或 device 映射；单用户局域网环境接受该权限模型
- 定期备份 `/app/data` 和 `/etc/cups`
