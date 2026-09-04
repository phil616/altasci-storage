# HTTP API

Base URL 为 `/api/v1`。JSON request body 上限为 1 MiB；Local 文件正文 endpoint 按 upload session 的 `expected_size` 流式限制。时间均为 RFC3339 UTC，ID 均为 UUIDv7。

认证 API 使用 `credentials: include`。除 GET/HEAD/OPTIONS 外，已登录接口必须携带由 `GET /auth/csrf` 获取的 `X-CSRF-Token`。错误统一为：

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to perform this operation.",
    "request_id": "..."
  }
}
```

## Auth

| Method | Path | 说明 |
|---|---|---|
| POST | `/auth/login` | 密码登录并建立 opaque session |
| POST | `/auth/logout` | 撤销当前 session |
| GET | `/auth/me` | 当前用户 |
| GET | `/auth/csrf` | 轮换并返回 CSRF token |
| GET | `/auth/oidc/providers` | 已启用 Provider 的公开名称与 ID |
| POST | `/auth/change-password` | 修改密码并撤销全部 session |
| GET | `/auth/oidc/{provider_id}/start` | OIDC Code + PKCE S256 开始 |
| GET | `/auth/oidc/{provider_id}/callback` | OIDC callback |

`POST /auth/login` 接受 `{ "email": "...", "password": "...", "remember": false }`。`remember` 默认为 `false`，此时服务端返回不含 `Max-Age` 的浏览器会话 Cookie；设为 `true` 时，Cookie 才按 `auth.session_absolute_timeout` 持久化（默认 7 天）。OIDC 登录使用 `/auth/oidc/{provider_id}/start?remember=true` 选择同一策略。无论是否持久化，服务端空闲超时、绝对超时和主动退出撤销始终生效。

## Project 与文件

| Method | Path | 说明 |
|---|---|---|
| GET/POST | `/projects` | 列表 / 创建项目 |
| GET/PATCH/DELETE | `/projects/{project_id}` | 查看 / 管理 / 异步删除 |
| GET | `/storage-backends` | 可供新项目选择的已启用 Backend（不含配置和 Secret） |
| GET | `/projects/{project_id}/nodes?parent_id=` | SQLite 目录列表 |
| POST | `/projects/{project_id}/directories` | 创建目录 |
| GET/PATCH/DELETE | `/nodes/{node_id}` | 查看 / rename 或 move / 删除 |
| POST | `/nodes/{node_id}/download` | Local URL 或外部预签名 URL |
| GET/HEAD | `/nodes/{node_id}/content` | Local streaming，支持单 Range |
| GET | `/projects/{project_id}/members` | 成员列表 |
| PUT/DELETE | `/projects/{project_id}/members/{user_id}` | 设置 / 移除成员 |

项目创建后 `storage_backend_id` 不可修改。

## Upload

| Method | Path | 说明 |
|---|---|---|
| POST | `/projects/{project_id}/uploads` | 创建 pending Blob 和 upload session |
| POST | `/uploads/{upload_id}/parts/presign` | 每次 1–100 个 multipart URL |
| PUT | `/uploads/{upload_id}/content` | 仅 Local streaming upload |
| POST | `/uploads/{upload_id}/complete` | Multipart complete、HEAD/Size 校验和 SQLite commit |
| DELETE | `/uploads/{upload_id}` | Abort |

创建参数只有 `parent_id`、`filename`、`size`、`mime_type`、`overwrite`。客户端不能提交 Object Key。100 MiB 是 multipart 切换阈值而不是文件上限；默认 part size 16 MiB，必要时自动增大以确保最多 10,000 parts。外部存储的数据始终由浏览器通过预签名 URL 直接上传，不经过 API 中转。预签名响应的 `headers` 始终为 JSON object；没有签名请求头时返回 `{}`。

## Share

登录接口：`GET /shares`、`POST /nodes/{node_id}/shares`、`GET/PATCH/DELETE /shares/{share_id}`。

公开接口使用独立认证，不经过普通 session middleware：

| Method | Path | 说明 |
|---|---|---|
| GET | `/public/shares/{token}/` | 分享元数据 |
| POST | `/public/shares/{token}/verify` | 校验 code，返回 30 分钟 Share Grant |
| GET | `/public/shares/{token}/nodes?parent_id=` | 浏览 target 或 descendant |
| POST | `/public/shares/{token}/nodes/{node_id}/download` | 匿名下载授权 |
| GET | `/public/shares/{token}/nodes/{node_id}/content` | Local 匿名 streaming |

新建分享返回一次性的 4 位数字 code；历史分享元数据中的 `code_length=8` 用于兼容旧版提取码。需要 code 的后续请求使用 `Authorization: Bearer <share-grant>`。Grant 只应保存在 React memory。外部匿名下载 URL 默认最多 120 秒，并受分享剩余有效期限制。

## Admin

- Users：`GET/POST /admin/users`，`GET/PATCH /admin/users/{id}`，`POST .../reset-password`，`POST .../revoke-sessions`。
- 管理员转移：`POST /admin/transfer`，要求当前管理员密码并撤销双方 session。
- Storage：`GET/POST /admin/storage-backends`，`GET/PATCH/DELETE /admin/storage-backends/{id}`，`POST .../test`。列表返回 `project_count` 和 `blob_count`；PATCH 不允许改变存储类型，省略 Secret 时保留原凭据；仍被项目或对象引用的后端返回 `STORAGE_BACKEND_IN_USE`，不得强制删除。
- OIDC：`GET/POST /admin/oidc-providers`，`GET/PATCH/DELETE /admin/oidc-providers/{id}`，`POST .../test`。
- Settings：`GET/PATCH /admin/settings`。

Storage/OIDC GET 从不返回明文 Secret。Storage connection test 执行 PUT、HEAD、GET、DELETE，不依赖 ListBucket。

OIDC 推荐使用两阶段配置：先创建 `enabled=false` 的草稿（此时 `client_id` 可以为空），再将 `https://<public-api>/api/v1/auth/oidc/{provider_id}/callback` 精确注册到 IdP，随后通过 PATCH 保存 Client ID/Secret、测试 Discovery 并启用。启用时必须存在 Client ID。Provider 必须先停用才能删除；删除会事务性移除其 `external_identities` 绑定和未完成的授权流程，但保留用户账户。

设置中的时间值使用 JSON 秒数。下载预签名有效期限制为 60–3600 秒，公开分享下载默认 120 秒。

## Health

`GET /health/live` 只检查进程；`GET /health/ready` 检查 SQLite 与 migration。
