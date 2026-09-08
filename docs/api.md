# HTTP API 使用说明

完整的路径、参数、请求体和响应模型以 [OpenAPI 3.1 规范](openapi.yaml) 为准。后端 API 前缀为 `/api/v1`，健康检查位于 `/health/live` 和 `/health/ready`。

## 通用约定

- JSON 请求体最大为 1 MiB，未知字段会被拒绝；
- 时间使用 RFC3339 UTC，资源 ID 使用 UUID；
- 每个响应包含 `X-Request-ID`，也可由可信代理传入合法的 `X-Request-ID`；
- 错误响应统一使用以下结构：

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to perform this operation.",
    "request_id": "..."
  }
}
```

## 会话和 CSRF

浏览器调用登录态 API 时必须使用 `credentials: include`。登录成功后，服务端设置 Secure、HttpOnly 的 `__Host-altasci_session` Cookie，并在响应中返回 CSRF token。

除 GET、HEAD 和 OPTIONS 外，登录态接口还要求：

```http
Origin: https://web.example.com
X-CSRF-Token: <GET /api/v1/auth/csrf 返回的值>
```

POST、PUT 和 PATCH 还必须声明受支持的 `Content-Type`。JSON 操作（包括没有业务请求体的操作）使用 `application/json`，Local 文件正文上传使用实际 MIME type 或 `application/octet-stream`。

`remember` 默认是 `false`，此时 Cookie 随浏览器会话结束；用户选择保持登录后才设置持久化 Cookie。服务端的空闲超时、绝对超时和主动撤销始终生效。

## 上传和下载

创建上传只接受 `parent_id`、`filename`、`size`、`mime_type` 和 `overwrite`。客户端不能指定 Object Key。

- Local：通过后端返回的 `/uploads/{uploadID}/content` 流式上传；
- 小于 100 MiB 的 S3/OSS 文件：通过预签名 URL 单次直传；
- 从 100 MiB 起的 S3/OSS 文件：multipart 上传，默认 part 为 16 MiB，并自动控制在最多 10,000 parts；
- 单文件业务上限为 5 TiB；100 MiB 是 multipart 切换阈值，不是文件大小上限。

浏览器向预签名 URL 发起请求时，必须原样携带 API 返回的 `method` 和 `headers`。Bucket 仍需单独允许前端 Origin 的 GET、HEAD、PUT CORS，并暴露 `ETag`。

## 公开分享

公开分享接口不使用普通 Session。新分享的提取码为 4 位数字；旧数据可能返回 `code_length: 8`。验证成功后返回有效期 30 分钟的 Share Grant，需要提取码的后续请求使用：

```http
Authorization: Bearer <share-grant>
```

Share Grant 应只保存在页面内存中。

## 管理接口

`/api/v1/admin/*` 同时要求有效 Session、CSRF（非安全方法）和管理员角色。Storage/OIDC 的 Secret 只允许写入，读取接口只返回 `has_secret` 或 `has_client_secret`。存储后端仍被项目或对象记录引用时不能删除；OIDC Provider 必须先停用再删除。

## 自动化接入

项目和文件接口支持 Bearer API 密钥。密钥管理、Scope、项目范围和 curl 示例见 [API 密钥接入](api-keys.md)。浏览器会话继续使用现有 Cookie + CSRF 认证。
