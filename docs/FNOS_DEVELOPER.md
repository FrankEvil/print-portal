# 成为 fnOS 开发者并上架应用

官方入口：

- 飞牛应用开放平台：<https://developer.fnnas.com/>
- 开发文档：<https://developer.fnnas.com/docs/>
- 快速开始：<https://developer.fnnas.com/docs/guide/quick-start/>

> `conversun/fnos-apps` 是第三方 `.fpk` 打包仓库，不是官方应用商店后台。  
> 官方上架需要在飞牛应用开放平台注册开发者，并按 `fnpack` / `appcenter-cli` 流程提交。

## 1. 注册开发者

1. 打开 <https://developer.fnnas.com/>
2. 使用飞牛账号登录；没有账号先注册
3. 进入「我的应用」或「注册成为开发者」
4. 填写开发者资料：
   - 开发者名称 / 团队名称
   - 联系方式
   - 应用方向与简介
5. 按平台要求完成开发者认证（个人或企业）

具体审核要求以平台页面为准；开发者资料通过后才能创建和提交正式应用。

## 2. 安装官方工具链

平台开发指南要求：

- `fnpack`：应用打包工具
- `appcenter-cli`：fnOS 应用中心命令行工具

常用命令：

```bash
fnpack create <appname> -t docker
fnpack build
fnpack build -d <app-dir>
```

`appcenter-cli` 用于在 fnOS 设备上安装测试：

```bash
appcenter-cli install-fpk /tmp/print-portal_0.1.0_x86.fpk
appcenter-cli start print-portal
appcenter-cli status print-portal
```

工具下载地址与版本以官方开发文档「开发工具」页面为准。

## 3. 本项目的应用包

本仓库已准备 fnOS 应用包模板：

- [deployments/fnos-app/print-portal](deployments/fnos-app/print-portal)
- [docs/FNOS_PACKAGE.md](FNOS_PACKAGE.md)

正式上架前需要：

- [ ] 构建并推送公开 Docker 镜像
- [ ] 替换 `docker/docker-compose.yaml` 中的镜像占位地址
- [ ] 确认 `service_port`（默认 8088）与端口规则
- [ ] 准备 64×64 / 256×256 图标（模板已生成，可替换为正式图标）
- [ ] 在真实 fnOS 设备安装并测试
- [ ] 填写应用详情、截图、隐私说明
- [ ] 提交官方审核

## 4. 本应用的特殊审核点

Print Portal 需要：

- `network_mode: host`：保留 USB、mDNS、局域网发现
- `/dev/bus/usb`：访问 USB 打印机
- `privileged: true`：当前模板为保证 CUPS/USB 可用而设置

官方文档说明 **Root 权限模式仅适用于飞牛官方合作的企业开发者**。  
容器 `privileged: true` 和宿主网络可能成为第三方上架审核的重点，提交前需要：

1. 向飞牛开发者平台确认第三方 Docker 应用是否允许该权限组合
2. 优先尝试改为 `devices: /dev/bus/usb` + 最小 capability，而不是完整 privileged
3. 在应用描述和安装向导中明确说明 USB 打印机权限用途
4. 如果官方审核不允许，需要走企业开发者/合作应用流程，或改为非 USB 的网络打印方案

## 5. 官方上架流程摘要

```text
注册开发者
 → 创建应用
 → 使用 fnpack 打包 .fpk
 → 在 fnOS 真机/虚拟机安装测试
 → 上传应用信息、图标、截图、隐私说明
 → 提交审核
 → 审核通过后上架官方应用中心
```

## 6. 与第三方仓库的区别

| 项目 | conversun/fnos-apps | 飞牛官方应用中心 |
|---|---|---|
| 性质 | 第三方打包仓库 | 官方商店 |
| 上架方式 | GitHub Release / 第三方应用中心 | 开发者平台提交审核 |
| 审核 | 仓库 CI | 官方审核 |
| 分发 | `.fpk` / 第三方商店 | 官方 App Center |
| 适合 | 快速测试、社区分发 | 正式面向 fnOS 用户 |

建议先用第三方仓库完成 `.fpk` 实机验证，再申请官方开发者上架。

## 7. 飞牛先锋开发者上架申请单字段

申请单：<https://trim-nas.feishu.cn/share/base/form/shrcn9ewk59TfoFWVAPMPy6aWSc>

建议填写：

| 字段 | 建议值 |
|---|---|
| 上架类型 | 新应用上架 |
| 平台 | x86 |
| fpk | `dist/print-portal_0.1.0_x86.fpk` |
| 系统 | fnOS |
| 截图（辅助审核） | `docs/screenshots/print-desktop.png`、`print-mobile.png`、`admin-printers.png` |
| 应用名称 | Print Portal / 打印门户（不能以「飞牛」开头） |
| 发布者 | 你的发布者名称 |
| 微信昵称/联系方式 | 你的实际联系方式（必填） |
| 开发者 | Print Portal contributors（开源项目） |
| 版本号 | `0.1.0`（必须三段式） |
| 备注 | 单用户局域网打印门户：内置 CUPS，网页上传即打印；支持 PPD / RAW 双队列、文件预览、页码范围、任务管理、驱动管理；需要 USB 打印机。 |

### 表单自查项

官方申请单明确说明：

1. **root 权限应用无法上架**：当前模板的 Docker Compose 使用 `privileged: true`，提交前必须向平台确认是否允许，或改为非 privileged / 企业合作流程
2. 鉴权优先接入飞牛统一网关，不要监听公网端口；当前应用有自己的 `ADMIN_TOKEN`，需要评估是否满足审核要求
3. `platform` 只能是 `x86`、`arm` 或 `all`，且要真机验证
4. 文件读写接口要校验路径，避免路径穿越
5. 版本号必须三段式，例如 `0.1.0`
6. 应用名称不能以「飞牛」开头
7. 应用详情页截图必填（不会显示在应用中心）

### 当前提交状态

- [x] 应用包模板
- [x] 64/256 图标
- [x] `.fpk` 结构验证
- [x] GHCR 镜像 workflow 构建成功（`ghcr.io/frankevil/print-portal:0.1.0`）
- [ ] 确认 `privileged: true` 审核政策
- [ ] 用最终镜像重新生成 fpk
- [ ] 准备应用详情页截图
- [ ] 填写发布者 / 微信联系方式
- [ ] 提交申请单

