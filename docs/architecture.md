# 架构

## 边界

系统由两个独立部署单元组成：

1. React Web 只负责交互和调用 API，不读取 SQLite，不持有长期认证 Token 或 Storage Credential。
2. Go Backend + SQLite 是身份、权限、项目、目录、文件名、分享和配置的唯一事实来源。
3. Local/S3/OSS 只保存以 `v1/objects/{project_id}/{blob_id}` 命名的二进制对象。

## Frontend

Frontend 使用一套统一的 Ant Design 6 组件与 Token 主题系统，TanStack Query 管理服务端状态，React Router 管理受保护路由、管理路由和公开分享路由。业务页面按路由懒加载，React、UI 和 Query 依赖独立分包。

表单负责字段级和跨字段校验，但 Backend 始终重复执行安全校验。公开 URL/CORS 使用精确 HTTPS Origin 输入，可信代理使用可增删的 CIDR 列表；Storage 和 OIDC Secret 只能写入、不能从 API 回显。前端从 Vite 构建环境变量 `VITE_API_BASE_URL` 读取公开的 API Origin，未设置时默认使用 `https://loopback-api.altasci.com`，不把 Secret 或认证 Token写入 Web Storage。

S3/OSS 的普通上传下载走 Browser ↔ Storage Provider。Backend 只在完成上传时执行 `HEAD` 并校验对象存在且 Size 一致。Local 是明确的数据面例外，通过 Backend 流式传输，并使用临时文件、`fsync` 和 atomic rename 提交到配置的文件系统；后端不会区分持久化磁盘与 tmpfs。

## 权限

管理员具有隐式全项目权限。普通用户读取项目需要 `project_members` 记录；写入同时要求：

```text
users.write_enabled = true
AND project_members.permission = write
```

所有 Handler 调用集中式 Authorization Service，Frontend 隐藏按钮仅用于改善体验。

## 文件与事务

目录来自 `fs_nodes` 的 parent relation，绝不通过 Bucket List 或 Object Key Prefix 重建。重命名、移动只修改 SQLite。覆盖文件创建新 Blob，完成后在一个 SQLite transaction 中交换 `current_blob_id` 并将旧 Blob 放入删除任务。

删除先在 transaction 中软删除节点、标记 Blob `deleting` 并写入 `background_jobs`；远程 Delete 在 transaction 外执行。指数退避为 1m、5m、15m、1h、6h，十次失败后进入 `failed`。

## 安全

- 密码：Argon2id，19 MiB、2 iterations、parallelism 1。
- 普通会话：256-bit opaque token；SQLite 只保存 SHA-256；`__Host-altasci_session` Cookie 使用 Secure、HttpOnly、SameSite=Lax。默认签发浏览器会话 Cookie，只有用户勾选“7 天内保持登录”时才设置持久化 `Max-Age`。
- Unsafe authenticated request：精确 Origin + `X-CSRF-Token`。
- Storage/OIDC Secret：AES-256-GCM；AAD 绑定记录 ID 和版本。
- Share URL token：192-bit；SQLite 只保存 SHA-256。
- Share code：新分享使用 4 位纯数字并以 Argon2id 保存；旧版 8 位提取码通过 `code_length` 兼容；IP 与 Share 双层持久化限流。
- Public Share Grant：内存保存的短时 HS256 token，仅有 `typ/share_id/iat/exp`。

## 单实例约束

V1 只支持一个 Backend 实例。不得把 SQLite 放在 NFS/SMB/对象存储上供多实例共享。健康检查的 readiness 只依赖 SQLite 和 migration，不因某个存储后端暂时离线而下线整个 API。

## API 密钥委托

密钥仅存摘要，以独立上下文区分会话与 API 身份；每次请求读取用户和密钥当前状态。路由显式声明 Scope 并从项目、节点或上传会话解析项目范围，再复用现有授权服务校验实时用户权限。有效权限取三者交集，项目列表同步过滤。密钥及管理接口仅接受会话，避免自动化凭证自行扩大授权。数据模型、迁移和预签名 URL 的撤销边界见 [API 密钥接入](api-keys.md)。
