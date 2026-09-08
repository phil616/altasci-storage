# 自动化 API 接入

登录后进入主导航「API 密钥」，为每个自动化程序创建独立密钥。选择操作权限、项目范围和有效期（最长 366 天），保存创建后显示的完整密钥。页面支持查看最近使用时间、编辑权限和有效期、撤销密钥。已撤销密钥不能恢复；轮换时创建新密钥，更新程序后撤销旧密钥。

## 认证与权限

现有 `/api/v1` 项目和文件接口接受请求头 `Authorization: Bearer <密钥>`。API 请求不需要 Cookie、Origin 或 CSRF Token。仅接受请求头中的密钥；不要把密钥放在 URL 中。显式 Authorization 无效时返回 401，即使同时有有效会话 Cookie 也不会回退。

有效权限 = 用户当前权限 ∩ 密钥操作权限 ∩ 密钥项目范围。每次请求重新读取密钥和用户，重新检查项目权限。用户停用、成员移除或写权限被收回，会阻止后续对应操作；重新启用用户后，未过期且未撤销的密钥可继续使用。密钥撤销、过期返回 401；操作或项目范围不允许返回 403。已经通过授权的进行中请求不会被强制中断。

| Scope | 允许的接口 / 操作 | 用户权限要求 |
| --- | --- | --- |
| `projects:read` | GET `/projects`、`/projects/{projectID}` | 当前可读；列表只返回授权范围内的项目 |
| `projects:create` | GET `/storage-backends`、POST `/projects` | 用户开启写入或管理员；密钥必须选择所有项目 |
| `projects:write` | PATCH `/projects/{projectID}` | 管理员 |
| `projects:delete` | DELETE `/projects/{projectID}` | 管理员 |
| `files:read` | 列目录、查询节点、生成下载地址、GET/HEAD 文件内容 | 当前项目可读 |
| `files:write` | 创建目录、上传/覆盖文件、重命名/移动节点、分片签名、提交/取消上传 | 当前项目可写且用户开启写入，或管理员 |
| `files:delete` | 删除文件或目录（含子目录） | 当前项目可写且用户开启写入，或管理员 |

Scope 独立生效，写入不自动包含读取或删除。文件覆盖属于 `files:write`。文件移动仍受现有同项目目录约束。上传操作沿用现有上传会话所属用户规则。

「所有项目」指当前及未来用户有权访问的项目，不赋予额外用户权限。「指定项目」最多 100 个，不可为空，创建/编辑时校验用户当前权限。创建项目需要所有项目范围，以避免新项目超出固定授权列表。

账户、密钥、项目成员、分享和系统管理接口只接受登录会话，即使管理员的 API 密钥也不能使用这些接口。

## 请求示例

把密钥保存在自动化平台的 Secret 中，并注入 `ALTASCI_API_KEY` 环境变量。下面使用 curl；解析响应的示例另需 jq。

```bash
API_BASE=https://api.example.com
curl --fail-with-body -H "Authorization: Bearer $ALTASCI_API_KEY" \
  "$API_BASE/api/v1/projects"

PROJECT_ID=your-project-id
curl --fail-with-body -H "Authorization: Bearer $ALTASCI_API_KEY" \
  "$API_BASE/api/v1/projects/$PROJECT_ID/nodes"

curl --fail-with-body -X POST \
  -H "Authorization: Bearer $ALTASCI_API_KEY" -H 'Content-Type: application/json' \
  -d '{"name":"自动备份"}' \
  "$API_BASE/api/v1/projects/$PROJECT_ID/directories"
```

本地存储上传示例（所需权限：`files:write`）：

```bash
printf hello > hello.txt
UPLOAD=$(curl --fail-with-body -X POST \
  -H "Authorization: Bearer $ALTASCI_API_KEY" -H 'Content-Type: application/json' \
  -d '{"filename":"hello.txt","size":5,"mime_type":"text/plain","overwrite":false}' \
  "$API_BASE/api/v1/projects/$PROJECT_ID/uploads")
UPLOAD_ID=$(printf '%s' "$UPLOAD" | jq -r .upload_id)
curl --fail-with-body -X PUT -H "Authorization: Bearer $ALTASCI_API_KEY" \
  --data-binary @hello.txt "$API_BASE/api/v1/uploads/$UPLOAD_ID/content"
curl --fail-with-body -X POST \
  -H "Authorization: Bearer $ALTASCI_API_KEY" -H 'Content-Type: application/json' \
  -d '{"parts":[]}' "$API_BASE/api/v1/uploads/$UPLOAD_ID/complete"
```

本地文件下载地址仍需携带 Bearer 请求头。S3/OSS 上传、下载返回的外部预签名 URL 已含短时授权，不要向外部存储发送 API 密钥。预签名 URL 在其有效期内独立生效，撤销 API 密钥不会撤销已签发的存储 URL；后续签名和上传提交仍会重新鉴权。应通过现有存储签名 TTL 设置限制该窗口。

## 密钥管理 HTTP 合约

这些接口要求登录 Cookie；写操作还要求可信 Origin 和 X-CSRF-Token。

- `GET /api/v1/api-keys`：返回 `{ "items": [...] }`，只列出自己的密钥元数据，包含过期和撤销的记录。
- `POST /api/v1/api-keys`：创建，201 返回元数据和只出现一次的 `token`。
- `PUT /api/v1/api-keys/{keyID}`：完整替换名称、权限、项目范围、有效期，200 返回元数据。请求字段同创建。可延长已过期密钥，不能恢复已撤销密钥。
- `DELETE /api/v1/api-keys/{keyID}`：永久撤销，204；重复撤销自己的密钥仍为 204。他人的密钥 ID 返回 404。

创建/替换请求：

```json
{
  "name": "每日备份",
  "scopes": ["projects:read", "files:read"],
  "all_projects": false,
  "project_ids": ["project-uuid"],
  "expires_at": "2026-10-01T00:00:00Z"
}
```

`expires_at` 使用未来 366 天内的 RFC3339 时间。名称为 1–100 个字符。未知字段、未知或重复 Scope、重复项目及矛盾的项目范围会被拒绝。更新是完整替换，多个页面同时保存时后一次成功保存生效。

## 存储与升级

新增 `00002_api_keys.sql` 前向迁移，由现有数据库迁移流程自动应用，无需重建用户或项目。密钥包含 256 位密码学随机数据，数据库仅保存 SHA-256 摘要及用于识别的短前缀，不保存可恢复的明文。列表、更新响应和审计不含密钥明文。创建、修改、撤销记录审计；现有文件/项目审计附带 API 密钥 ID。

实现分为 repository 密钥持久化、HTTP 认证/Scope 与项目范围校验、现有 authorization 用户权限检查。新增可供自动化使用的路由时必须显式配置 `apiAccess(scope)`，管理路由使用 `sessionOnly`，不得仅根据 HTTP 动词推断权限（下载签名也使用 POST）。
