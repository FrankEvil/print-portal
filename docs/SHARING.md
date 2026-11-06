# 分享给别人使用

本文是一份“把 Print Portal 交给另一位用户部署和使用”的清单。

## 1. 需要提供什么

分享给他人时，建议提供：

- 本项目源码压缩包或 Git 仓库地址
- 对方的打印机型号
- 一份 `ADMIN_TOKEN`（不要使用默认 `demo123`）
- 部署方式：
  - 对方使用 Docker：提供 `docker-compose.yml` 或 `docker-compose.fnos.yml`
  - 对方使用 fnOS：按 [DEPLOYMENT.md](DEPLOYMENT.md) 的 fnOS 章节操作
- 使用说明：[USER_GUIDE.md](USER_GUIDE.md)

不要直接分享你自己的生产 `ADMIN_TOKEN`；让对方生成新的 Token。

## 2. 对方需要准备

- 一台局域网内长期在线的 Linux 主机 / NAS
- Docker 与 Docker Compose
- 打印机已连接（USB 或同一局域网）
- 主机固定局域网 IP

## 3. 对方部署步骤

```bash
# 1. 解压项目
cd print-portal

# 2. 修改 Token
export ADMIN_TOKEN='请替换为随机长字符串'

# 3. 启动
docker compose up -d --build

# 4. 检查
curl http://127.0.0.1:8088/healthz
```

然后打开：

```text
http://<对方主机 IP>:8088/
```

## 4. 对方添加打印机

参考 [USER_GUIDE.md](USER_GUIDE.md)：

1. 管理后台输入 `ADMIN_TOKEN`
2. 添加打印机
3. USB 选 USB 设备；网络打印机选扫描到的设备
4. 支持 IPP Everywhere 时勾选免驱
5. 保存后创建 PPD + RAW 两个队列

## 5. 给对方的使用地址

在管理后台「共享」页可以复制：

```text
Web：http://<主机 IP>:8088/
PPD：ipp://<主机 IP>:8631/printers/<ppd 队列>
RAW：ipp://<主机 IP>:8631/printers/<raw 队列>
```

- 手机 / 临时电脑：直接用 Web
- 已安装厂商驱动的电脑：使用 RAW
- 免驱 / IPP Everywhere：使用 PPD

## 6. 安全提醒

- 不要让对方的 8631 直接暴露到公网
- 让对方在自己的防火墙限制 8631 只允许局域网
- 不要使用默认 `ADMIN_TOKEN`
- README 和文档中的 IP 只是示例，以对方实际网络为准
- 如果对方需要公网访问，只反代 Web 8080/8088，不要反代 8631

## 7. 最小交付清单

- [ ] 源码 / 镜像
- [ ] `ADMIN_TOKEN`
- [ ] `README.md`
- [ ] `docs/USER_GUIDE.md`
- [ ] `docs/DEPLOYMENT.md`
- [ ] `docs/FAQ.md`
- [ ] 对方打印机型号与驱动确认
- [ ] 局域网 IP / 防火墙确认
