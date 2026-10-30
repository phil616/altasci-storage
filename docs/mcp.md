# MCP 智能体接入

后端直接提供 `https://<API域名>/mcp`，与 REST API 使用同一服务、数据库和 API Key。启动新版后端即生效，不需要额外进程或数据库迁移。反向代理需要将 `/mcp` 的 POST 请求及 Authorization、Accept、Content-Type 和 `MCP-*` 头转发到后端。

## 协议与认证

使用[官方 Go SDK v1.8.0](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0) 的无状态 Streamable HTTP，支持当前稳定协议 [2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)，并兼容 `2025-11-25` 等 SDK 支持的旧协议。新协议通过 `server/discover` 发现服务，每次请求携带 `_meta`；旧客户端可使用 `initialize`。协议解析、版本协商、工具 Schema 校验和标准错误由 SDK 处理。没有旧式 `/sse`、独立消息端点或服务端持久 MCP 会话。

按本项目的静态凭证要求，认证为 `Authorization: Bearer <API_KEY>`。每个 HTTP 请求都必须带密钥，包括服务发现、工具列表和工具调用。Cookie、查询参数中的密钥和 MCP Session ID 均不能替代认证。原有 API Key 的存储、到期、撤销和最近使用记录直接复用。

[MCP 官方授权规范](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)描述的是可选的 OAuth 授权流程。本端点采用预先配置的静态 Bearer API Key，不实现 OAuth 登录、动态客户端注册或 OAuth 发现；客户端需支持自定义 Authorization 请求头。仅支持 OAuth 登录的客户端不能直接接入。

生产使用 HTTPS。在 Web 端「API 密钥」创建专用密钥，选择所需 Scope、项目范围和有效期。只读智能体通常选择 `projects:read`、`files:read`；需要写入时增加 `files:write`，需要删除时单独增加 `files:delete`。

有效权限始终为：**用户当前权限 ∩ 密钥 Scope ∩ 密钥项目范围**。撤销/到期/用户停用阻止后续请求；修改密钥范围、移除项目成员或关闭用户写权限也会在后续调用生效。已经授权的进行中请求不强制终止。即使密钥属于管理员，也不会增加工具种类。工具列表固定展示下表白名单，具体调用还会执行授权。

无需 Cookie 或 CSRF。非浏览器客户端可以省略 Origin；带 Origin 的请求必须匹配 `cors.allowed_origins`，否则返回 403。允许的浏览器来源可通过 CORS 发送 Authorization 和协议使用的 `MCP-Protocol-Version`、`MCP-Method`、`MCP-Name`。

## 客户端配置

在支持远程 HTTP MCP 和静态请求头的客户端填写：

| 配置项 | 值 |
| --- | --- |
| URL | `https://api.example.com/mcp` |
| Transport | Streamable HTTP / HTTP |
| Authorization 请求头 | `Bearer <在 Web 端创建的 API Key>` |

将密钥放入客户端 Secret 或其支持的环境变量引用中，避免提交到仓库。配置 JSON 的字段由具体客户端决定，服务端无需为不同智能体新增工具或账户。

使用 curl 检查新协议发现；`ALTASCI_API_KEY` 应由 Secret 注入：

```bash
API_BASE=https://api.example.com
curl --fail-with-body "$API_BASE/mcp" \
  -H "Authorization: Bearer $ALTASCI_API_KEY" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' \
  -H 'MCP-Method: server/discover' \
  --data-binary '{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}'
```

列出工具时将 `MCP-Method` 和 JSON 中的 `method` 都改为 `tools/list`，保留 `_meta`。现代 SDK 会自动处理这些元数据、请求头和版本协商，不需要应用手动拼接 JSON-RPC。

## 工具与权限边界

| 工具 | Scope | 参数与行为 |
| --- | --- | --- |
| `list_projects` | `projects:read` | `{}`，只返回用户和密钥共同可访问的项目 |
| `get_project` | `projects:read` | `project_id`，查询项目 |
| `list_nodes` | `files:read` | `project_id`，可选 `parent_id`；省略为根目录 |
| `get_node` | `files:read` | `node_id`，查询文件/目录元数据 |
| `read_file` | `files:read` | `node_id`，可选 `offset`、`limit`、`encoding`，读取文件内容 |
| `get_download_url` | `files:read` | `node_id`，获取 Local 内容 URL 或 S3/OSS 预签名 URL |
| `create_directory` | `files:write` | `project_id`、`name`，可选 `parent_id` |
| `update_node` | `files:write` | `node_id`，至少提供 `name` 或 `parent_id`，重命名/同项目移动 |
| `delete_node` | `files:delete` | `node_id`，删除文件或目录及其后代 |
| `create_upload` | `files:write` | `project_id`、`filename`、`size`，可选 `parent_id`、`mime_type`、`overwrite` |
| `upload_content` | `files:write` | `upload_id`、`content`，可选 `encoding`，写入自己的待提交小文件 |
| `presign_upload_parts` | `files:write` | `upload_id`、`part_numbers`，生成分片上传 URL |
| `complete_upload` | `files:write` | `upload_id`，可选 `parts: [{part_number, etag}]`，提交上传 |
| `abort_upload` | `files:write` | `upload_id`，取消上传 |

写入不隐含读取或删除权限；覆盖文件使用 `files:write`。节点和上传 ID 都会解析所属项目再校验密钥范围，不能通过猜测 ID 绕过授权。移动目标必须在同一项目。上传类工具只能操作当前用户自己的上传，即使管理员密钥也没有操作他人上传的例外。

MCP 不提供账户/密码修改、API Key 管理、退出登录、分享管理、项目创建/修改/删除、成员管理、用户管理、存储后端配置、OIDC、系统设置等工具。账户和管理员管理继续通过 Web 端完成。没有通用 HTTP 代理、任意 SQL、执行命令或宿主机路径访问工具。

工具具有结构化输入 Schema，未知参数会被拒绝，并带只读/破坏性提示。成功返回 MCP 内容及 `structuredContent`；业务失败返回 `isError: true`，文本内包含现有错误码和 request ID。无效工具或协议请求由 SDK 返回 MCP 错误。现有审计继续包含 API Key ID，不记录密钥明文。HTTP 返回 `Cache-Control: no-store`，新协议可缓存结果声明 `cacheScope: private`、`ttlMs: 0`。

## 文件传输

小文件上传顺序：

1. 调用 `create_upload`，例如 `{"project_id":"...","filename":"hello.txt","size":5,"mime_type":"text/plain"}`，取得 `upload_id`。
2. 调用 `upload_content`，传 `{"upload_id":"...","content":"hello"}`。
3. 调用 `complete_upload`，传 `{"upload_id":"..."}`，得到文件节点 ID。
4. 调用 `read_file`，传 `{"node_id":"..."}`，返回文本内容。

`upload_content` 支持 Local、S3 和 OSS 的 local/single 上传，解码后最多 1 MiB，字节数必须等于创建时的 `size`。默认 `encoding: "utf8"`；二进制使用 `encoding: "base64"` 和标准 base64 内容。请求体总上限为 2 MiB，包含 JSON 和编码开销；大量需要 JSON 转义的文本可改用 base64。上传写入后仍需提交，不自动完成。

`read_file` 支持所有存储后端，默认读取 64 KiB，每次最多 1 MiB。`offset`、`limit` 按字节计，响应包含 `content`、`encoding`、`offset`、`bytes_read`、`size`、`eof`。二进制或切断 UTF-8 字符边界的范围需选择 base64。可递增 offset 分次读取，或使用下载 URL。

大文件沿用 `create_upload` 返回的 HTTP URL、方法及签名请求头传输，再 `complete_upload`。S3/OSS 的 multipart 上传需先 `presign_upload_parts`，完成后提交各分片 ETag；Local 大文件使用现有内容接口。Local 内容 URL 需要带本项目的 Bearer API Key；**外部存储预签名 URL 不得携带该 API Key**。已经签发的存储 URL 在 TTL 内独立有效，撤销密钥不使已有 URL 立即失效，后续签名和上传提交仍会重新鉴权。

## 并发测试

无需外部服务即可运行（使用真实 TCP HTTP、SQLite WAL 和临时 Local 存储）：

```bash
cd backend
go test ./...
go vet ./...
go test -race ./internal/httpapi ./internal/database -run 'TestMCP|TestConcurrent' -count=1 -v
```

覆盖官方 SDK 的 `2026-07-28` 和 `2025-11-25` 接入、管理员工具白名单、Scope 和项目范围、节点/上传 ID 越权、管理员操作他人上传被拒、同客户端实时撤权、未知参数、Origin、Cookie/URL 密钥拒绝、请求上限、UTF-8/base64 和 1 MiB 传输边界。

并发场景包含：32 个任务共享两个独立用户的 SDK 客户端，完成 64 次“创建上传→写入→提交→读取→删除”，加上跨项目及跨用户节点访问拒绝，共 448 次工具调用；同名目录创建和同一上传提交各有 32 个同时竞争的请求，断言恰好一次成功、其余为冲突，且不会生成重复节点。数据库另有 32 个并发读改写事务的回归测试。

SQLite 显式事务使用 `BEGIN IMMEDIATE`，解决并发“先读后写”事务升级时的 `database is locked`，保留 WAL 读并发和 10 连接池。测试记录本机耗时；它验证并发正确性与身份隔离，不代表生产吞吐保证。S3/OSS 实际网络和凭证需另外配置现有存储合约测试，见[测试说明](build.md#测试范围与文档一致性)。
