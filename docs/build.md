# 编译与运行

## 为什么不直接使用 `go run`

`go run` 会先下载所需 Go toolchain 和 modules，再在临时目录编译。首次执行可能数分钟没有应用日志，看起来像卡死；编译完成后临时二进制也不会保留。因此项目提供显式 Makefile，生产和首次运行都应先构建持久二进制。

初始化读取密码时终端不会显示星号或字符，这是防止密码泄露的正常行为。CLI 会在进入隐藏输入前明确输出提示。

## 依赖

- Go 1.27 或更高版本，并允许 `GOTOOLCHAIN=auto` 获取 `go.mod` 指定的工具链；
- C compiler 与 CGO，SQLite driver 编译需要；
- Node.js 20.19+（20.x）或 22.12+，与锁定的 Vite 8 engines 一致；
- npm。

先检查环境：

```bash
make doctor
```

## 构建

构建全部产物：

```bash
make build
```

Frontend 也可以独立构建和本地预览生产产物：

```bash
cd frontend
npm ci
npm run build
npm run preview
```

前端采用按路由懒加载，Vite/Rolldown 依据模块依赖图自动分包，没有固定手工 vendor 分组。`npm run build` 先执行 TypeScript 类型检查，再执行生产打包。失败时旧的 `frontend/dist/` 可能仍在，不应把目录存在当作本次构建成功。

执行完整静态检查、Backend 测试和 Frontend 浏览器烟雾测试：

```bash
make test
```

未提供部署环境变量时，Playwright 会先构建 `dist`，再启动 Vite Preview Server，以实际生产 chunks 和受控 API 响应检查配置错误页、结构化设置校验和移动端导航；提供 `E2E_BASE_URL`、`E2E_ADMIN_EMAIL`、`E2E_ADMIN_PASSWORD` 后还会验证真实部署环境的管理员登录路径。

只构建后端：

```bash
make build-backend
./dist/altasci-server version
./dist/altasci-server --help
```

产物位置：

```text
dist/altasci-server   Backend binary for the build target
frontend/dist/        Frontend static files
```

构建命令默认使用 `go build -v`，npm 安装也启用了 info 日志；它们会打印当前阶段、正在处理的 package 和最终产物路径。由于仓库可能尚无 Git commit，后端构建关闭 VCS stamping，避免无关的 Git fatal 信息。需要查看每一条 Go/CGO 底层命令时可以运行：

```bash
cd backend
go build -x -o ../dist/altasci-server ./cmd/altasci-server
```

也可以通过 Makefile 开启同样的完整跟踪：

```bash
make build-backend GO_BUILD_FLAGS=-x
```

## 初始化

从仓库根目录执行：

```bash
make init INIT_ARGS="--admin-email admin@example.com --public-web-url https://web.example.com --public-api-url https://web-api.example.com"
```

也可以直接运行二进制：

```bash
cd backend
../dist/altasci-server init \
  --admin-email admin@example.com \
  --public-web-url https://web.example.com \
  --public-api-url https://web-api.example.com
```

CLI 会依次打印配置、密码、数据库 migration、master key 和原子初始化阶段。密码必须直接在真实交互式终端中输入，不能从命令行参数、pipe 或 IDE 的非交互 Task 提供。

## 启动和健康检查

```bash
make run
```

或：

```bash
cd backend
../dist/altasci-server serve --config config.toml
```

另开终端检查：

```bash
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/health/ready
```

正常响应分别为：

```json
{"status":"ok"}
{"status":"ready"}
```

## 常见问题

### 停在下载或编译阶段

执行 `make doctor`，再用 `go build -x` 查看具体停在 toolchain、module 下载还是 C compiler。企业网络环境还应检查 `GOPROXY`、`GOSUMDB` 和出站网络策略。

### 停在密码输入阶段

字符不回显是正常的。输入至少 12 个字符并按 Enter，再输入一次确认。如果出现 `password must be read from an interactive TTY`，请在普通终端直接执行，不要使用 pipe、后台任务或没有 TTY 的运行器。

### 启动立即退出

错误会写到 stderr。常见原因是尚未执行 `init`、当前工作目录不对，或 `config.toml` 中的数据库/master key 路径不可访问。使用上面的 `make run` 可确保工作目录正确。

### 前端显示“前端环境配置不可用”

检查启动/构建环境中的 `VITE_API_BASE_URL` 及 `frontend/.env`、`.env.local`、对应模式的环境文件，地址必须是精确 HTTPS Origin：

```dotenv
VITE_API_BASE_URL=https://web-api.example.com
```

不要填写 `http://`、路径、查询参数或结尾斜杠。没有配置或值为空时默认使用 `https://loopback-api.altasci.com`。进程环境变量会覆盖 `.env` 文件；修改后重启开发服务，或重新构建并部署整个前端目录。如果同时修改后台的公开 URL，则需要重启 Backend。

### Nginx 中出现 `useAccessibility` / `ESC` JavaScript 错误

这表示生产 chunks 在浏览器中发生了模块初始化顺序问题，或部署目录混用了两次构建。当前构建让 Rolldown依据完整模块图自动切分 Ant Design 依赖，禁止再用 `maxSize` 强制拆开 `antd` / `@rc-component` 内部模块。请用最新的 `frontend/dist/` 完整替换旧静态目录，并确认 `index.html` 不缓存、`/assets/` 缺失时返回 `404`。不要只复制新的 `index.html` 或只覆盖部分 assets。

## 管理员密码恢复

在后端工作目录、交互式终端运行：

```bash
cd backend
../dist/altasci-server admin-reset-password --config config.toml
```

命令修改当前 active 管理员的本地密码并撤销该管理员全部 Session，不撤销 API Key。密码要求 12–128 个 Unicode 字符。`serve` 是省略子命令时的默认行为；完整 CLI 入口见 `backend/cmd/altasci-server/main.go`。

## 测试范围与文档一致性

初次执行浏览器测试前：

```bash
cd frontend
npm ci
npx playwright install chromium
```

Linux 若缺少浏览器系统依赖，可按运行环境安装依赖，或使用 Playwright 的 `install --with-deps chromium`。`make test` 不自动安装 npm 依赖或浏览器。

| 检查 | 命令 / 配置 | 覆盖与限制 |
| --- | --- | --- |
| 后端测试 | `cd backend && go test ./...` | repository、认证授权、HTTP 集成、Local 存储等 |
| 后端静态检查 | `cd backend && go vet ./...` | Go 静态分析 |
| 文档契约 | `cd backend && go test ./internal/httpapi -run TestOpenAPIContract -count=1` | 实际 chi 路由与 OpenAPI 的方法/路径、唯一 operationId、引用、资源响应字段、设置键、Scope 枚举及路由 Scope |
| 前端构建 | `cd frontend && npm run build` | TypeScript 和生产打包 |
| 本地浏览器 | `cd frontend && npm run test:e2e` | 默认在 127.0.0.1:4173 启动生产 Preview，构建 API Origin 固定为 https://api.example.test，由测试拦截 |
| 真实部署浏览器 | `E2E_BASE_URL`、`E2E_ADMIN_EMAIL`、`E2E_ADMIN_PASSWORD` | 管理员登录和管理页烟雾测试；设 E2E_BASE_URL 后不启动本地 Preview，本地模拟测试跳过 |
| S3 合约 | `ALTASCI_TEST_S3_CONFIG_JSON`、`ALTASCI_TEST_S3_SECRET_JSON` | 缺少任一变量时跳过；配置结构见配置参考 |
| OSS 合约 | `ALTASCI_TEST_OSS_CONFIG_JSON`、`ALTASCI_TEST_OSS_SECRET_JSON` | 缺少任一变量时跳过；会实际创建并清理测试对象 |

完整 `make test` 会先构建前端，而本地 Playwright webServer 还会再构建一次。指定真实部署环境但缺少管理员凭据时，部署登录测试也会跳过，不能把“无失败”理解为已验证真实部署。

改接口时同步修改 Handler 和 `docs/openapi.yaml`，改默认值或状态机时同步修改配置/架构文档，改前端分享交互时同步更新 API 使用说明。契约测试能发现路由遗漏、失效引用、资源响应字段及 Scope 漂移，不能证明所有字段和运行时行为一致；请求字段、响应序列化、鉴权及错误路径仍需人工对照源码和集成测试。

## 仓库内的部署入口

`.cnb.yml` 目前在 main 分支 push 时使用 `node:20` 安装依赖，构建时显式设置 `VITE_API_BASE_URL=https://storage-api.altasci.com`，将 `frontend/dist/` 发布到 EdgeOne Pages 项目 `altasci-storage`。构建镜像须满足 Node 20.19+；配置未锁定 Node 小版本和 EdgeOne CLI 版本。流水线有部署锁和成功/失败通知，但没有执行 `make test`，也没有构建或部署 Go 后端。通知凭据来自流水线的 Secret 导入。

`deploy-local.sh` 是当前维护环境的便捷脚本：使用 loopback API 地址构建前端，然后清空并复制到固定的 1Panel 静态目录。它包含 sudo 操作、固定路径，且不是原子发布或通用安装器。其它环境应按[部署文档](deployment.md)配置自己的发布过程。
