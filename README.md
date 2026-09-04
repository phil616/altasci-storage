# AltasCI Network Storage

AltasCI 云盘是一个 SQLite 元数据驱动的虚拟文件系统，文件正文存储在 Local、标准 S3 或 Alibaba Cloud OSS 中。React 前端和 Go API 独立构建、独立部署；S3/OSS 上传下载通过短时预签名 URL 直连，不经过 API 服务器。

## 项目结构

- `backend/`：Go 控制面、SQLite migration、Storage Adapter、后台任务与测试。
- `frontend/`：React 19 + TypeScript + Vite 8 + Ant Design 6 单页应用。
- `docs/`：架构、API 与部署说明。
- `PLAN.md`：V1 设计基线和强制约束。

## 编译

不要用 `go run` 判断项目是否卡死：首次执行会先下载 Go toolchain/modules 并临时编译，在应用真正启动前可能长时间没有日志，而且不会留下可复用二进制。建议从仓库根目录执行：

```bash
make doctor
make build
```

构建产物：

- `dist/altasci-server`：Backend Linux 可执行文件；
- `frontend/dist/`：Frontend 静态部署文件。

检查二进制：

```bash
./dist/altasci-server version
./dist/altasci-server --help
```

完整依赖、详细构建过程和卡死排查见 [编译与运行文档](docs/build.md)。

## 本地验证

```bash
cd backend
go test ./...
go vet ./...

cd ../frontend
npm ci
npm run build
```

生产基线按 `PLAN.md` 使用 Go 1.27+。前端需要 Node.js 22.12+（建议 Node.js 24）。

S3/OSS 契约测试默认在无凭据环境跳过。要对真实 Private Bucket 执行同一套 Put、Head、Range Download、Delete、Presign 和 Multipart Abort 契约测试，可设置 `ALTASCI_TEST_S3_CONFIG_JSON` / `ALTASCI_TEST_S3_SECRET_JSON` 或对应的 `ALTASCI_TEST_OSS_*` 环境变量。

## 首次初始化

在交互式终端运行：

```bash
make init INIT_ARGS="--admin-email admin@example.com --public-web-url https://loopback.altasci.com --public-api-url https://loopback-api.altasci.com"
```

公开 URL 必须是 HTTPS Origin；`http://localhost` 会按 `PLAN.md` 的 HTTPS-only 要求被拒绝。

如果 `config.toml` 不存在，初始化命令会先生成包含安全默认路径的 Bootstrap Config；如需自定义监听地址、数据库或 master key 路径，可以预先复制并修改 `config.example.toml`，或通过 `--config` 指定文件。

密码只从真实 TTY 读取，不接受命令行密码参数，因此不要通过无交互的 CI、IDE Task 输入重定向或后台命令执行首次初始化。初始化会创建 SQLite、forward-only migration、32-byte master key、唯一管理员和精确 API CORS Origin。

随后启动 API：

```bash
make run
```

本地检查:
```bash
sudo cp -r frontend/dist /opt/1panel/www/sites/loopback.altasci.com/index
```

健康检查：

```bash
curl http://127.0.0.1:8080/health/live
curl http://127.0.0.1:8080/health/ready
```

前端部署时修改 `frontend/public/config.json` 中的 `apiBaseUrl`。该值和后台的公开 Web/API URL、CORS Origin 都必须是无路径、无结尾斜杠的精确 HTTPS Origin，其中不得放入任何存储或 OIDC Secret。

详细步骤见 [部署文档](docs/deployment.md)，API 见 [接口文档](docs/api.md)。
