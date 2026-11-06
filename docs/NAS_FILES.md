# 使用 NAS 上的文件打印

目前 Print Portal 的网页打印页是「上传客户端文件 → 服务器保存 → 提交打印」，还没有内置 NAS 文件选择器。

## 方式一：把 NAS 共享目录挂载到容器

这是最简单、最可控的方式。

在 compose 中增加只读挂载：

```yaml
volumes:
  - /vol2/1000/共享目录:/nas:ro
```

推荐约定：

```text
容器内根目录：/nas
```

然后由应用增加以下能力：

- `GET /api/nas/list?path=...`：列出目录
- `POST /api/print/nas`：提交 `/nas` 下的文件
- 所有路径都限制在 `/nas` 内，拒绝 `..`、绝对路径逃逸和符号链接逃逸
- 只允许打印支持的文件类型

这种方式不依赖 fnOS API，普通 Docker 和 fnOS 都能用。

## 方式二：fnOS 文件选择器 / 授权

fnOS 开放 API 提供前端 JS SDK：

```text
@trimjs/web-app
pickUserFile()
openAppAuth()
```

需要：

- `manifest` 声明 `micro_app=true`
- `config/resource` 声明 `trim.file.userAccess` scope
- 应用通过统一网关运行
- 回调 `redirectUri` 和建议的 `app.sock` 同域

得到用户授权路径后，后端再读取文件。这个方式体验最好，但需要额外接入 SDK 和授权回调。

## 方式三：文件管理器“打开方式”

也在 `app/ui/config` 注册文件入口：

```json
{
  "myapp.editor": {
    "type": "iframe",
    "protocol": "http",
    "port": "8080",
    "url": "/edit",
    "fileTypes": ["pdf", "docx", "xlsx", "pptx", "jpg", "png"],
    "noDisplay": true
  }
}
```

fnOS 会追加：

```text
?path=/vol1/Users/admin/Documents/example.pdf
```

应用需要读取 `path`，并校验路径权限。

## 建议顺序

1. 普通 Docker / fnOS 内部测试：先做方式一，挂载共享目录 + 路径校验
2. fnOS 正式体验：再接入方式二的 `pickUserFile` / `openAppAuth`
3. 需要在文件管理器右键打开：最后加方式三
