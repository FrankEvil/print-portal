# internal/api

HTTP 路由、请求校验、JSON/SSE 响应。

P1 需要新增：

- `GET /api/printers`
- `POST /api/print`
- `GET /api/jobs`
- `DELETE /api/jobs/{id}`
- `/api/admin/*`（`X-Admin-Token` 保护）
