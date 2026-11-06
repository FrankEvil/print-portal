# 认证模式

Print Portal 支持两种运行形态，由环境变量 `PRINT_PORTAL_AUTH_MODE` 控制。

## 1. 独立版：Token 登录

```env
PRINT_PORTAL_AUTH_MODE=token
ADMIN_TOKEN=<访问令牌>
```

流程：

```text
打开 / → 输入访问 Token
 → POST /api/auth/login 校验
 → Token 保存到当前浏览器 sessionStorage
 → 后续 API 自动携带 X-Admin-Token
```

行为：

- 所有 `/api/*` 都需要认证
- 未登录访问任意页面会跳转到登录页
- 管理接口仍需要管理员权限
- 登录页地址：`/admin/login`
- 适合普通 Docker / 非 fnOS 部署

## 2. fnOS 版：飞牛统一网关认证

```env
PRINT_PORTAL_AUTH_MODE=fnos
PRINTD_SOCKET=/run/print-portal/app.sock
```

应用需要以 fnOS 统一网关入口运行：

```text
fnOS 用户 → 飞牛统一网关 → app.sock → printd
```

网关转发请求时携带：

```text
X-Trim-Userid
X-Trim-Isadmin
X-Trim-Username
```

行为：

- `X-Trim-Userid` 缺失：API 返回 401
- `X-Trim-Isadmin != true`：管理 API 返回 403
- 不需要用户输入 `ADMIN_TOKEN`
- 必须先登录 fnOS
- 不应直接暴露 8088 端口给公网

应用包：

- [deployments/fnos-app/print-portal](deployments/fnos-app/print-portal)
- `ui/config` 使用 `gatewayPrefix` + `gatewaySocket`
- `docker-compose.yaml` 设置 `PRINT_PORTAL_AUTH_MODE=fnos`

## 3. 两种模式对比

| 项目 | token | fnos |
|---|---|---|
| 登录方式 | Print Portal Token | fnOS 登录态 |
| 鉴权 Header | `X-Admin-Token` | `X-Trim-Userid` |
| 管理权限 | ADMIN_TOKEN | `X-Trim-Isadmin=true` |
| 入口 | 8088 端口 | fnOS 统一网关 Unix Socket |
| 适合 | 普通 Docker / 本地 | fnOS 应用中心 |

## 4. 构建与部署

独立版：

```bash
docker compose -f docker-compose.local.yml up -d
```

fnOS 版：

```bash
docker compose -f docker-compose.fnos.yml up -d
```

当前 `docker-compose.fnos.yml` 默认使用 `token`，接入 fnOS 应用包后再切换为 `fnos`。
