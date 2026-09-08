# AltasCI 云盘

AltasCI 云盘是一个面向企业内部使用的文件管理系统。SQLite 保存用户、权限、目录和文件元数据，文件内容可存储在本地目录、兼容 S3 的对象存储或阿里云 OSS。前端与后端独立构建和部署；S3/OSS 上传下载使用短时预签名 URL。

## 主要能力

- 项目、目录、文件和成员权限管理；
- Local、S3、Alibaba Cloud OSS 存储后端；
- 大文件分片上传、范围下载和异步删除；
- 4 位数字提取码、有效期和公开分享；
- 本地密码、OIDC、CSRF、限流与审计；
- 用户 API 密钥、操作权限、项目范围、到期和撤销管理；
- React 19 + Ant Design 前端和 Go HTTP API。

## 快速开始

需要 Go 1.27+、支持 CGO 的 C 编译器、Node.js 22.12+ 和 npm。

```bash
make doctor
make build
make init INIT_ARGS="--admin-email admin@example.com --public-web-url https://web.example.com --public-api-url https://api.example.com"
make run
```

`init` 必须在交互式终端运行，密码输入不会回显。它会创建 `backend/config.toml`、SQLite 数据库、master key 和首个管理员。公开 Web/API URL 必须是没有路径和结尾斜杠的 HTTPS Origin。

构建产物：

- `dist/altasci-server`：后端可执行文件；
- `frontend/dist/`：前端静态文件。

启动后可检查：

```bash
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/health/ready
```

前端默认连接 `https://loopback-api.altasci.com`（开发和生产相同）。需要覆盖时，在 `frontend/.env` 中设置 `VITE_API_BASE_URL=https://your-api.example.com`，或在启动/构建时传入同名环境变量（优先级更高）：

```bash
cd frontend
VITE_API_BASE_URL=https://your-api.example.com npm run dev
# 生产构建
VITE_API_BASE_URL=https://your-api.example.com npm run build
```

可参考 `frontend/.env.example`。修改配置后需重启开发服务或重新构建生产版本；详见[部署说明](docs/deployment.md)。

## 文档

- [编译、初始化与排错](docs/build.md)
- [部署、可信 URL 与对象存储 CORS](docs/deployment.md)
- [架构与安全边界](docs/architecture.md)
- [自动化 API 密钥接入](docs/api-keys.md)
- [HTTP API 使用说明](docs/api.md)
- [OpenAPI 3.1 规范](docs/openapi.yaml)

## 验证

```bash
make test
```

该命令执行后端测试与静态检查、前端生产构建和浏览器端到端测试。
