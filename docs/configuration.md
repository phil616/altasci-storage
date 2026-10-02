# 配置参考

本页对照 `backend/cmd/altasci-server/main.go`、`internal/config/config.go`、`internal/httpapi/{admin,settings,auth,files,shares}.go` 和前端设置表单。时间配置的单位均为秒。

## 三层配置

| 来源 | 配置 | 生效方式 |
| --- | --- | --- |
| Bootstrap TOML | `server.listen`、`database.path`、`security.master_key_file` | 后端启动时读取 |
| SQLite `system_settings` | 公开地址、CORS、超时和限流 | 见下表；由管理员 API/UI 修改 |
| Vite 环境 | `VITE_API_BASE_URL` | 开发服务启动或生产构建时读取 |

TOML 示例见 [config.example.toml](../backend/config.example.toml)。`--config` 默认是当前工作目录下的 `config.toml`；TOML 内相对路径也相对于进程工作目录，不是配置文件目录。没有用环境变量覆盖后端 TOML 的实现。前端环境变量是公开的构建配置，不能存放密钥。

## 系统设置

通过 `GET /api/v1/admin/settings` 读取键值对象，通过 `PATCH` 提交需要修改的键；成功返回 204。

| 键 | 初始化默认值 | 实际生效规则 |
| --- | --- | --- |
| `site.public_web_url` | `init --public-web-url` | 精确 HTTPS Origin；修改后重启后端，影响分享链接与 OIDC 返回地址 |
| `site.public_api_url` | `init --public-api-url` | 精确 HTTPS Origin；修改后重启后端，影响 Local 传输 URL 与 OIDC 回调 |
| `cors.allowed_origins` | `[公开 Web Origin]` | 非空 HTTPS Origin 数组；每个请求读取 |
| `auth.session_idle_timeout` | 86400（1 天） | 运行时有效范围 300–2592000（5 分钟–30 天） |
| `auth.session_absolute_timeout` | 604800（7 天） | 运行时有效范围 3600–7776000（1 小时–90 天） |
| `storage.upload_presign_ttl` | 900（15 分钟） | 运行时有效范围 60–3600 |
| `storage.download_presign_ttl` | 300（5 分钟） | 运行时有效范围 60–3600 |
| `share.default_expiration` | 604800（7 天） | 新建分享且未指定有效期时使用；范围 60–315360000（3650 天） |
| `share.download_presign_ttl` | 120（2 分钟） | 范围 30–600；再取与分享剩余有效期的较小值 |
| `security.trusted_proxy_cidrs` | `[]` | 每请求读取；只信任直接 TCP 对端所属 CIDR |
| `security.login_rate_limit` | 见下文 | 每次密码登录读取 |
| `security.share_rate_limit` | 见下文 | 每次分享提取码验证读取 |

保存时间配置时，API 只检查 JSON 数值不小于 1；读取时若超出上表范围，会使用默认值，**不是截断到边界值**。修改超时不会重写已签发 URL、既有分享的到期时间或现有会话的绝对到期时间。会话创建时读取两种超时；会话超过 5 分钟未更新 `last_seen_at` 时，根据当前空闲超时续期，且不超过绝对到期时间。

设置 PATCH 逐键保存，没有跨键事务，也不保证处理顺序。混合提交合法和非法键可能部分成功；失败后应重新 GET 确认。前端额外要求 CORS 包含公开 Web Origin、空闲超时不超过绝对超时等；不要把前端校验当作后端的跨字段约束。

## 限流

初始化默认值：

```json
{
  "security.login_rate_limit": {
    "attempts": 10,
    "window_seconds": 600,
    "cooldown_seconds": 900
  },
  "security.share_rate_limit": {
    "ip_attempts": 5,
    "ip_window_seconds": 60,
    "ip_ban_seconds": 900,
    "escalated_ban_seconds": 3600,
    "share_attempts": 50,
    "share_window_seconds": 600,
    "share_ban_seconds": 900
  }
}
```

登录按 `客户端 IP + 规范化邮箱` 计数。保存校验要求次数至少 1、时间至少 10；运行时另要求次数不超过 100、窗口和冷却不超过 86400，超出则整组回退默认值。触发封禁的那次错误登录仍返回 401（可带 `Retry-After`），封禁期间后续请求返回 429。

分享同时按 `客户端 IP + 分享 token 摘要` 和分享 token 摘要计数。次数至少 1、时间至少 10，升级封禁时间必须不小于普通 IP 封禁时间。同一 IP/分享在 24 小时历史窗口内第三次触发封禁时使用升级时长。验证成功清除对应 IP/分享维度记录，不清除分享全局失败记录。状态持久化到 SQLite，重启不清除封禁。

## 存储后端配置

通过 `/api/v1/admin/storage-backends` 管理，和 `system_settings` 分开存储。以下为创建请求；`enabled` 省略时为 false。

```json
{
  "name": "本地文件",
  "type": "local",
  "enabled": true,
  "config": { "root_path": "/srv/altasci/objects" }
}
```

```json
{
  "name": "S3 文件",
  "type": "s3",
  "enabled": true,
  "config": {
    "region": "us-east-1",
    "bucket": "example-private-bucket",
    "endpoint": "https://s3.example.com",
    "prefix": "altasci",
    "force_path_style": true
  },
  "secret": { "access_key_id": "REPLACE_ME", "secret_access_key": "REPLACE_ME" }
}
```

OSS 使用 `type: "aliyun_oss"`，配置字段是 `region`、`bucket`、可选 `endpoint`、`prefix`、`use_cname`；Secret 为 `access_key_id` 和 `access_key_secret`。S3/OSS 都要求 region、bucket 和两项凭据；endpoint 省略时使用 SDK 默认地址。对象的实际远端键为可选 prefix 加 `v1/objects/{projectID}/{blobID}`。

存储 PATCH 必须提供 `name`、`config`；应同时提供 `enabled`（省略会设为 false），不能修改 `type`。省略或传 `secret: null` 保留旧密钥。修改 root、bucket 或 prefix 不会迁移已有对象。连接测试会创建临时对象并尝试 PUT/HEAD/GET/DELETE，成功不等于浏览器 CORS 已正确配置。

## OIDC 配置

创建和 PATCH 共用字段：`name`、HTTPS `issuer`、`client_id`、`client_secret`、`scopes`、`enabled`、`auto_create_user`、`auto_link_verified_email`、`allowed_email_domains`。`name` 和 `issuer` 必需；启用时必须有 `client_id`。scopes 省略或空白时为 `openid email profile`，且必须包含 `openid`。

PATCH 按完整配置替换非密钥字段；省略布尔值会设为 false。`client_secret` 省略或空字符串保留原值；读取只返回 `has_client_secret`。邮箱域数组为空或 null 表示不限制域，非空时忽略大小写进行精确域匹配。测试接口只执行 discovery，不测试完整用户登录。回调配置及域名切换流程见[部署文档](deployment.md)。
