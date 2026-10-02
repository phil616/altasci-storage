# HTTP API 使用说明

后端业务前缀为 `/api/v1`，健康检查位于 `/health/live` 和 `/health/ready`。所有已注册方法、参数、请求体和响应模型见 [OpenAPI 3.1](openapi.yaml)。本文说明调用顺序、权限和实现中的边界；自动化示例见 [API 密钥接入](api-keys.md)。

## 通用约定

- 使用 `decodeJSON` 的请求体限制为 1 MiB，只接受一个 JSON 值，拒绝未知结构字段；超限/格式错误均返回 400 `MALFORMED_JSON`。Local 二进制上传不适用此限制。存储 config/secret 和设置值使用独立解析，嵌套未知字段不统一拒绝。
- 资源 ID 为 UUID；时间响应是 UTC RFC3339，可带小数秒。大小和 Range 单位是字节。
- 列表通常返回 `{ "items": [] }`；系统设置直接返回键值对象。当前没有分页、排序或搜索查询协议，前端表格分页是在已加载列表上进行。
- 请求未使用的 query 参数通常被忽略；应只传接口声明的参数。
- `parent_id` 在节点响应中根目录为 null；创建目录/上传时省略或空字符串表示根目录，移动时 `"parent_id":""` 表示移到根目录。
- 后端响应设置 `X-Request-ID`、`Cache-Control: no-store`、`X-Content-Type-Options: nosniff`。仅可信代理传入且匹配 `[A-Za-z0-9._-]{8,128}` 的 request ID 被沿用。
- 业务错误使用下面的 JSON。路由器默认 404/405、代理错误和公共内容接口的 416 不保证此结构，客户端要处理非 JSON 响应。

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to perform this operation.",
    "request_id": "..."
  }
}
```

## 三种认证方式

| 凭证 | 使用范围 | 写请求额外要求 |
| --- | --- | --- |
| `__Host-altasci_session` Cookie | 登录后的业务、账户、密钥管理和后台接口 | 允许的 Origin + `X-CSRF-Token` |
| `Authorization: Bearer altasci_...` | 明确支持 API Key 的项目/文件接口 | 不要求 Cookie/Origin/CSRF；仍校验 Scope 和项目范围 |
| `Authorization: Bearer <share-grant>` | 需要密码的公开分享节点/下载接口 | 不要求会话/CSRF |

三种凭证不能互换。普通业务接口出现 Authorization 时优先走 API Key 认证，即使有 Cookie 也不回退。公开分享不使用 Session 或 API Key；无需密码的分享不需要 Grant。`/api/v1/admin/*` 还要求管理员角色。

浏览器会话调用使用 `credentials: "include"`。POST/PUT/PATCH 的 JSON 操作建议始终发送 `Content-Type: application/json`；即便没有业务请求体，也可发送 `{}` 以满足会话中间件检查。DELETE 不要求 Content-Type。Local PUT 使用文件 MIME type 或 `application/octet-stream`。实际会话中间件对非 Local 写操作也允许 `application/octet-stream`，但有 JSON 请求体的 Handler 仍按 JSON 解码。

CORS 只允许配置的精确 HTTPS Origin，允许的方法为 GET/HEAD/POST/PUT/PATCH/DELETE/OPTIONS，请求头为 Content-Type、X-CSRF-Token、Authorization、X-Request-ID。当前未配置 Access-Control-Expose-Headers，也未把 Range 加入允许请求头列表；跨域浏览器不保证能读取 X-Request-ID/Retry-After 或发送需预检的自定义 Range 请求。

## 登录、会话和 OIDC

| 方法与路径（省略 `/api/v1`） | 行为 |
| --- | --- |
| POST `/auth/login` | `{email,password,remember?}`；要求可信 Origin，不要求既有 CSRF；200 返回 `{user,csrf_token}` 并设置 Cookie |
| GET `/auth/me` | 当前用户；仅 Session |
| GET `/auth/csrf` | **轮换**当前 Session 的 CSRF token，返回 `{csrf_token}`；旧 token 失效 |
| POST `/auth/logout` | 撤销当前 Session，清除 Cookie；204 |
| POST `/auth/change-password` | `{current_password,new_password}`；成功撤销全部会话，清除 Cookie；204 |
| GET `/auth/oidc/providers` | 无需登录，返回启用 provider 的 `{items:[{id,name}]}` |
| GET `/auth/oidc/{providerID}/start` | `return_to` 可选，须与公开 Web 地址同 scheme/host，默认 `/projects` 的完整 Web URL；`remember=true` 可选；302 跳转 |
| GET `/auth/oidc/{providerID}/callback` | `state`、`code`；成功设置 Cookie 并 302 返回 Web；失败 401 `OIDC_FAILED` |

`remember` 默认 false，true 时 Cookie Max-Age 等于签发时绝对会话时长，默认 7 天。服务端空闲/绝对超时和撤销仍生效。修改或重置密码不会撤销 API Key；只有撤销会话也不影响 API Key。OIDC-only 用户不能通过 change-password 自行设置首个本地密码，该操作返回 `PASSWORD_NOT_SET`。

会话调用示例（API/WEB 替换为部署 Origin；以下代码在浏览器中执行）：

```js
const API = "https://api.example.com";
const login = await fetch(`${API}/api/v1/auth/login`, {
  method: "POST",
  credentials: "include",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password, remember: false }),
});
if (!login.ok) throw new Error(`Login failed: ${login.status}`);
const { csrf_token } = await login.json();
// Origin 由浏览器自动发送，必须在后端 CORS 允许列表中。
const created = await fetch(`${API}/api/v1/projects`, {
  method: "POST",
  credentials: "include",
  headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf_token },
  body: JSON.stringify({ name: "资料", storage_backend_id: backendID }),
});
```

## 项目和文件

以下表中带 Scope 的接口同时支持 Session 和 API Key；Scope 不会增加用户本身的权限。

| 方法与路径（省略 `/api/v1`） | 成功响应 | API Key Scope / 用户权限 |
| --- | --- | --- |
| GET `/storage-backends` | 200，启用后端的 id/name/type/enabled 列表 | `projects:create`；API Key 还要求可创建项目且 all_projects |
| GET `/projects` | 200，项目列表 | `projects:read`；按当前权限和密钥范围过滤 |
| POST `/projects` | 201，Project | `projects:create`；管理员或全局可写用户 |
| GET `/projects/{projectID}` | 200，Project | `projects:read`；可读 |
| PATCH `/projects/{projectID}` | 200，Project | `projects:write`；管理员 |
| DELETE `/projects/{projectID}` | 202，`{"status":"deleting"}` | `projects:delete`；管理员 |
| GET `/projects/{projectID}/members` | 200，成员列表 | 仅管理员 Session |
| PUT `/projects/{projectID}/members/{userID}` | 200，project_id/user_id/permission | 仅管理员 Session；`{permission:"read"或"write"}` |
| DELETE `/projects/{projectID}/members/{userID}` | 204 | 仅管理员 Session |
| GET `/projects/{projectID}/nodes?parent_id=...` | 200，当前层节点列表 | `files:read`；可读 |
| POST `/projects/{projectID}/directories` | 201，Node | `files:write`；可写；`{name,parent_id?}` |
| GET `/nodes/{nodeID}` | 200，Node | `files:read`；可读 |
| PATCH `/nodes/{nodeID}` | 200，Node | `files:write`；可写；name/parent_id 至少一项非 null |
| DELETE `/nodes/{nodeID}` | 204，子树已软删除，物理对象异步删除 | `files:delete`；可写 |
| POST `/nodes/{nodeID}/download` | 200，DownloadResponse | `files:read`；可读 |
| GET/HEAD `/nodes/{nodeID}/content` | 200；GET Range 可返回 206 | `files:read`；仅 Local |

创建项目接受 `name`、`storage_backend_id`、可选 `description`。项目 PATCH 接受 name/description，name 必填；省略 description 会清空它，不支持更换 storage_backend_id。普通用户创建项目得到 write 成员权限，成员管理仍只允许管理员。

文件名规则见[架构文档](architecture.md)：NFC、1–255 UTF-8 字节、禁止路径组件和控制字符。节点只能移到同项目目录，目录不能移入自己或后代；同级重名返回冲突。无效文件名当前由 repository 普通错误映射成 500 `INTERNAL`，不要假定所有参数错误都是 422。

## 上传协议

| 方法与路径（省略 `/api/v1`） | 请求 | 成功响应 |
| --- | --- | --- |
| POST `/projects/{projectID}/uploads` | `{filename,size,parent_id?,mime_type?,overwrite?}` | 201，CreateUploadResponse |
| POST `/uploads/{uploadID}/parts/presign` | `{part_numbers:[1,2]}`，每次 1–100 个，唯一且 1–10000 | 200，`{parts:[{part_number,url,method,headers,expires_at}]}` |
| PUT `/uploads/{uploadID}/content` | 原始二进制正文，仅 Local | 204 |
| POST `/uploads/{uploadID}/complete` | Local/single 使用 `{}` 或 `{parts:[]}`；multipart 使用完整 parts | 200，最终 Node |
| DELETE `/uploads/{uploadID}` | 无请求体 | 204，取消上传并安排对象清理 |

全部要求 `files:write`（使用 API Key 时）及项目写权限。上传会话后续操作仅允许创建者或管理员，同一用户的不同 API Key 仍须满足各自范围与 Scope。

创建响应公共字段：`upload_id`、`upload_type`、`delivery`、`expires_at`。会话有效 24 小时，与短时预签名 URL 有效期不同。

| upload_type | 条件 | 附加响应与客户端动作 |
| --- | --- | --- |
| `local` | Local 任意支持大小 | `delivery=local`、`url`、`method=PUT`；携带会话+CSRF或 API Key 上传正文 |
| `single` | S3/OSS 且 size < 100 MiB | `delivery=external`、url/method/headers/presign_expires_at；直接发到存储服务 |
| `multipart` | S3/OSS 且 size >= 100 MiB | `delivery=external`、part_size/part_count；按分片签名后上传 |

单文件范围 0–5 TiB。size 省略时 Go 按 0 处理，客户端应总是明确传入真实长度；mime_type 默认 application/octet-stream，overwrite 默认 false。100 MiB 是分片阈值，不是上限。默认 part_size 为 16 MiB，大文件会按 MiB 向上调整以控制在最多 10000 片。

完成 multipart 的请求例子：

```json
{"parts":[{"part_number":1,"etag":"provider-etag-1"},{"part_number":2,"etag":"provider-etag-2"}]}
```

保留 provider 返回的 ETag（包括需要的引号），后端排序并校验唯一编号和非空 ETag。只有完成后文件才出现在节点列表；后端确认对象存在、大小与预期一致，再提交元数据。重复 complete 会冲突，不保证幂等。连接中断后不要盲目重复提交，应先检查节点；失败需要重建上传时，要区分旧文件与本次上传结果。

外部 URL 使用返回的 method 和可由客户端设置的签名 headers，不发送 API Key 或 Session。浏览器控制的 Host/Content-Length 等头由浏览器按 URL/正文生成。Bucket CORS 需暴露 ETag。上传流程不提供查询/恢复上传会话的 GET 接口。

## 下载协议

先 POST download，得到 `{delivery,url,expires_at?}`，不是文件正文。external 为短时存储 URL；local 为需继续鉴权的 API URL，不包含 expires_at。

Local 支持单 Range：`bytes=0-1023`、`bytes=1024-`、`bytes=-1024`，不支持多段。普通内容 GET 返回 200/206 或 JSON 416 `RANGE_INVALID`；HEAD 返回完整文件长度而忽略 Range。内容类型是文件 MIME，`Content-Disposition: attachment`。公开内容没有 HEAD 路由，Range 无效返回无 JSON 正文的 416。

已签发的外部存储 URL 在有效期内独立生效；撤销会话、API Key 或分享不保证立刻撤销该 URL。后续向 API 申请 URL 仍重新鉴权。

## 分享管理和携带密码

| 方法与路径（省略 `/api/v1`） | 请求或行为 | 成功响应 |
| --- | --- | --- |
| POST `/nodes/{nodeID}/shares` | `{require_code?,expires_at?}`；需节点所在项目写权限 | 201，Share + url + 可选 code |
| GET `/shares` | 普通用户看自己创建的，管理员看全部；含过期/撤销记录 | 200，ShareList |
| GET `/shares/{shareID}` | 创建者或管理员 | 200，Share |
| PATCH `/shares/{shareID}` | 创建者或管理员；可改 expires_at、disabled | 200，Share |
| DELETE `/shares/{shareID}` | 创建者或管理员；撤销 | 204；重复 DELETE 返回 404 |

这些接口仅支持 Session。`require_code` 默认 true；新 code 为 4 位数字字符串，可有前导零。expires_at 省略使用默认时长，空字符串表示永不过期；非空须为未来 RFC3339。PATCH 未传或 null 表示保留原字段；`disabled:true` 撤销后不能用 false 恢复。URL 和 code **只在创建响应中返回**，详情和列表没有这两个字段，也没有修改提取码的接口。

“携带密码”是前端生成链接的选项，不是创建 API 的字段。后端返回普通链接，例如：

```json
{"url":"https://web.example.com/s/PUBLIC_TOKEN","code":"0123"}
```

上面只展示创建响应新增的两项，完整响应还有 Share 元数据。前端默认不勾选；勾选后生成并复制：

```text
https://web.example.com/s/PUBLIC_TOKEN?code=0123
```

客户端应使用 URL/URLSearchParams 编码，不能把 code 转成数值。页面 `/s/{token}` 读取 code，自动调用下述 verify，成功后显示分享文件列表；不会自动下载或在线预览文件。错误或限流会回到可手动修改的输入表单，不反复自动重试。无参数链接保持手动输入流程。密码仍由服务端验证，不通过 querystring 绕过鉴权。

`code` 只属于 **Web 页面 URL**；给后端 GET 元数据/节点接口附加 `?code=...` 不会获得授权。当前页面会保留地址栏参数，完整链接同时包含访问地址与明文提取码。

## 公开分享 API

| 方法与路径（省略 `/api/v1`） | 行为 |
| --- | --- |
| GET `/public/shares/{token}/` | 无需登录/提取码；返回 `{share,target,grant_required}`；注意末尾 `/` |
| POST `/public/shares/{token}/verify` | `{code:"0123"}`；200 返回 `{grant,expires_at}`，Grant 有效 30 分钟 |
| GET `/public/shares/{token}/nodes?parent_id=...` | 返回目标或目标子树的当前层；需密码时附带 Grant |
| POST `/public/shares/{token}/nodes/{nodeID}/download` | 返回 DownloadResponse；需密码时附带 Grant |
| GET `/public/shares/{token}/nodes/{nodeID}/content` | Local 内容；需密码时附带 Grant |

元数据可在验证前读取，包含目标名称等元信息。`grant_required=false` 时跳过 verify；verify 只用于需要提取码的分享。旧分享通过 `code_length=8` 标识，字符集合为 `23456789ABCDEFGHJKMNPQRSTUVWXYZ`；服务端去除首尾空白并转大写后验证。4 位分享只接受数字。

验证成功后：

```http
Authorization: Bearer <share-grant>
```

Grant 仅保存在页面内存中。token 路径对应分享，grant 必须绑定同一 share_id。默认 parent 为分享目标：文件目标返回该文件一项；目录目标返回其子项，禁止跨出子树。未找到或撤销通常返回 404 `SHARE_NOT_FOUND`，过期返回 410 `SHARE_EXPIRED`；verify 对无效/过期/撤销分享统一按无效提取码处理，返回 401 或触发限流后的 429。

分享绑定实时目标；更新文件或目录内容会反映在分享中，删除目标后不可浏览。公开访问校验分享状态和节点范围，不重新校验创建者当前成员关系。

## 管理接口

所有 `/admin/*` 路由要求管理员 Session，写操作要求 Origin/CSRF。完整字段见 OpenAPI；默认值和配置规则见[配置参考](configuration.md)。

| 路径（省略 `/api/v1/admin`） | 方法与作用 |
| --- | --- |
| `/users`、`/users/{userID}` | GET 列表/详情；POST 创建普通 active 用户（201）；PATCH 改邮箱、write_enabled、status（200） |
| `/users/{userID}/reset-password` | POST `{password}`，撤销该用户全部会话（204） |
| `/users/{userID}/revoke-sessions` | POST，撤销全部会话（204） |
| `/transfer` | POST `{new_admin_id,current_password}`，转移唯一管理员并清除当前 Cookie（204） |
| `/storage-backends`、`/storage-backends/{backendID}` | GET 列表/详情；POST 创建（201）；PATCH 配置（200）；DELETE 无引用后端（204） |
| `/storage-backends/{backendID}/test` | POST，尝试对象读写删除；200 `{"status":"ok"}` 或 502 |
| `/oidc-providers`、`/oidc-providers/{providerID}` | GET 列表/详情；POST 创建（201）；PATCH 完整配置（200）；DELETE 已停用 provider（204） |
| `/oidc-providers/{providerID}/test` | POST，OIDC discovery；200 `{"status":"ok"}` 或 502 |
| `/settings` | GET 键值对象；PATCH 指定键（204），非事务性更新 |

存储与 OIDC PATCH 并非任意字段的局部合并。密钥不回显，只返回 has_secret/has_client_secret；省略密钥保留原值。项目/对象记录仍引用存储时 DELETE 返回 409 `STORAGE_BACKEND_IN_USE`，软删除后也可能保留引用。启用状态的 OIDC provider 删除返回 409 `OIDC_PROVIDER_ENABLED`。

## 常见错误与重试

| HTTP | 常见 code | 处理 |
| --- | --- | --- |
| 400 | MALFORMED_JSON、RETURN_TO_INVALID | 修正请求格式或参数 |
| 401 | UNAUTHENTICATED、INVALID_CREDENTIALS、SHARE_CODE_INVALID、SHARE_GRANT_INVALID | 检查所用凭证类型、有效期或密码 |
| 403 | FORBIDDEN、ADMIN_REQUIRED、SESSION_REQUIRED、API_SCOPE_FORBIDDEN、API_PROJECT_FORBIDDEN、CSRF_ORIGIN、CSRF_INVALID | 检查用户/项目权限、Scope 和会话请求头 |
| 404 | NOT_FOUND、SHARE_NOT_FOUND、DIRECTORY_NOT_FOUND | 核实目标是否存在和分享是否撤销 |
| 409 | CONFLICT、UPLOAD_STATE_INVALID、STORAGE_BACKEND_IN_USE | 检查重名、上传状态或资源引用，不盲目重试 |
| 410 | SHARE_EXPIRED | 使用新的有效分享 |
| 415 | CONTENT_TYPE_INVALID | 补充支持的 Content-Type |
| 416 | RANGE_INVALID（普通 Local） | 改正字节区间 |
| 422 | UPLOAD_INVALID、UPLOAD_SIZE_MISMATCH、PARTS_INVALID、SETTING_INVALID、API_KEY_INVALID | 修正业务字段；有些上传失败会进入 failed 状态 |
| 429 | LOGIN_RATE_LIMITED、SHARE_RATE_LIMITED | 按 Retry-After 秒数等待；浏览器跨域读取该头受 CORS 限制 |
| 502/503 | STORAGE_PROVIDER_ERROR、STORAGE_UNAVAILABLE、NOT_READY | 检查存储配置、网络或数据库 |

错误表不是穷举；以具体响应 code 为准。API 没有通用幂等键协议，不能对所有 POST/DELETE 无条件重试。请求日志关联使用 X-Request-ID 或 JSON 中的 request_id。
