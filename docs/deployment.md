# 部署

## 独立部署

Backend 与 Frontend 必须使用不同构建产物和域名。Frontend `dist/` 不会嵌入 Go binary，Backend 也没有 SPA 静态资源路由。

生产要求：

- Go 1.27+；
- Node.js 22.12+（构建 Frontend）；
- HTTPS-only；
- 单 Backend 实例；
- SQLite 和生产用 Local Storage 放在本机持久化磁盘，不放 NFS/SMB；
- master key 文件权限 `0600`。

## Bootstrap config

```toml
[server]
listen = "127.0.0.1:8080"

[database]
path = "./data/altasci-network-storage.db"

[security]
master_key_file = "./data/master.key"
```

除监听地址、数据库路径和 master key 路径之外的设置都由 Admin UI 写入 SQLite。首次初始化命令见根目录 README。

首次执行 `init` 时如果配置文件不存在，服务会生成上述默认 Bootstrap Config。普通启动不会自动创建数据库；如果尚未初始化，会返回可操作的错误提示，避免遗留一个阻止后续初始化的空数据库。

## Frontend config

前端通过 Vite 环境变量 `VITE_API_BASE_URL` 读取公开 API Origin。开发和生产构建默认均为 `https://loopback-api.altasci.com`；没有配置或值为空时使用默认值。

在 `frontend/.env` 中覆盖（可从 `.env.example` 复制）：

```dotenv
VITE_API_BASE_URL=https://web-api.example.com
```

也可以在启动/构建命令中传入环境变量：

```bash
cd frontend
VITE_API_BASE_URL=https://web-api.example.com npm run dev
VITE_API_BASE_URL=https://web-api.example.com npm run build
```

优先级从高到低：进程环境变量 → `.env.[mode].local` → `.env.[mode]` → `.env.local` → `.env` → 默认地址。文件位于 `frontend/`；`npm run dev` 使用 development 模式，`npm run build` 使用 production 模式。

地址必须是精确 HTTPS Origin：只包含 `https://`、主机名和可选端口，不允许路径、查询参数、通配符或结尾斜杠。启动时会校验地址并在配置错误时显示诊断页。该变量是公开配置，会打包到浏览器代码中。

环境变量在 Vite 启动/构建时读取。修改后需重启开发服务，生产环境需重新构建并部署 `frontend/dist/`；仅修改静态服务器的环境变量或启动 `npm run preview` 不会改变已经构建的 API 地址。

Web server 必须将非静态路径回退到 `index.html`，并设置 HSTS、CSP、`X-Content-Type-Options: nosniff`、Referrer-Policy 和 Permissions-Policy。示例见 `frontend/nginx.conf`。

发布新版本时应完整、原子地替换静态目录，不能只覆盖部分文件。`index.html` 必须禁用缓存；带内容 hash 的 `/assets/` 可以长期缓存，但资源不存在时必须返回 `404`，不得回退到 `index.html`。这可避免浏览器把旧入口文件、新 chunks 或 HTML 响应混合使用。发布后如果浏览器仍持有故障版本，应清理站点/CDN缓存并强制刷新一次。

## 可信 URL 对照

假设 Web 为 `https://web.example.com`，API 为 `https://web-api.example.com`，各位置应配置为：

| 位置 | 配置项 | 值 |
|---|---|---|
| Frontend 构建环境 / `.env` | `VITE_API_BASE_URL` | `https://web-api.example.com` |
| Admin → 系统设置 | 公开 Web URL | `https://web.example.com` |
| Admin → 系统设置 | 公开 API URL | `https://web-api.example.com` |
| Admin → 系统设置 | 允许的 CORS Origins | 至少包含 `https://web.example.com` |
| Web 反向代理 | `server_name` / TLS 证书 | `web.example.com` |
| API 反向代理 | upstream | Backend 的 `127.0.0.1:8080` |
| OIDC Provider | Redirect URI | `https://web-api.example.com/api/v1/auth/oidc/{providerID}/callback` |

`security.trusted_proxy_cidrs` 不是可信 URL 列表。它只填写与 Backend 直接建立 TCP 连接的反向代理 IP/CIDR，例如同机 Nginx 使用 `127.0.0.1/32`；不要填写访客 IP、域名或 `0.0.0.0/0`。

更换域名时，为避免把当前管理页面锁在 CORS 之外：

1. 先准备新域名的 DNS、TLS 和反向代理；
2. 在“允许的 CORS Origins”中同时保留旧 Web Origin并加入新 Web Origin；
3. 修改公开 Web/API URL并保存，随后重启 Backend；
4. 修改 `VITE_API_BASE_URL`，重新构建并发布新 Frontend；
5. 从新域名验证登录、上传、下载和 OIDC，再删除旧 Origin。

## CORS

API 的 `cors.allowed_origins` 只能包含精确 HTTPS Origin。API 返回该 Origin、`Access-Control-Allow-Credentials: true` 和 `Vary: Origin`，不支持通配符 Credentials。管理界面会强制要求列表包含当前“公开 Web URL”，以减少保存后立即无法登录的风险。

S3/OSS Bucket 必须保持 Private，并另行配置 Browser CORS：允许 Web Origin 使用 GET、HEAD、PUT，允许必要 request headers，并暴露 `ETag` 和 provider request/checksum headers。应用凭据不需要修改 Bucket Policy/CORS 的权限。

## 反向代理

只有 `security.trusted_proxy_cidrs` 中的 TCP peer 才允许提供 `CF-Connecting-IP`、`X-Real-IP` 或 `X-Forwarded-For`。不要把 `0.0.0.0/0` 设为可信代理。

## 备份

必须分别保护：

1. 使用 SQLite Backup API 或 `sqlite3 database.db ".backup backup.db"` 得到的一致性数据库备份；
2. `master.key`；
3. bootstrap TOML；
4. Local 模式的 storage root。

不能在 WAL 活动时只复制 `.db`，也不能只备份对象存储。丢失 SQLite 会永久丢失文件名、目录、权限和分享关系；丢失 master key 会无法解密 Storage/OIDC Secret。

Local Storage 可以指向 tmpfs，后端不会区分内存和磁盘文件系统。但 tmpfs 卸载或主机重启后，对象会消失而 SQLite 元数据仍然存在，因此只适用于允许丢失数据的测试或临时环境。必须确保 tmpfs 在后端启动前完成挂载，避免路径被自动创建到磁盘。
