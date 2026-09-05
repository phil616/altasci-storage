# 编译与运行

## 为什么不直接使用 `go run`

`go run` 会先下载所需 Go toolchain 和 modules，再在临时目录编译。首次执行可能数分钟没有应用日志，看起来像卡死；编译完成后临时二进制也不会保留。因此项目提供显式 Makefile，生产和首次运行都应先构建持久二进制。

初始化读取密码时终端不会显示星号或字符，这是防止密码泄露的正常行为。新版 CLI 会在进入隐藏输入前明确输出提示。

## 依赖

- Go 1.27 或更高版本，并允许 `GOTOOLCHAIN=auto` 获取 `go.mod` 指定的工具链；
- C compiler 与 CGO，SQLite driver 编译需要；
- Node.js 22.12 或更高版本；
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

前端采用按路由懒加载，并将 React、Ant Design 和 TanStack Query 拆成独立 vendor chunks。`npm run build` 同时执行 TypeScript 类型检查；只有类型检查和生产打包都成功才会生成 `frontend/dist/`。

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
dist/altasci-server   Linux backend binary
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

### 前端显示“运行时配置不可用”

确认 Web 根目录存在 `/config.json`，内容是合法 JSON，且 `apiBaseUrl` 是精确 HTTPS Origin：

```json
{ "apiBaseUrl": "https://web-api.example.com" }
```

不要填写 `http://`、路径、查询参数或结尾斜杠。`config.json` 应设置 `Cache-Control: no-store`，修改后无需重新打包前端；如果同时修改后台的公开 URL，则需要重启 Backend。

### Nginx 中出现 `useAccessibility` / `ESC` JavaScript 错误

这表示生产 chunks 在浏览器中发生了模块初始化顺序问题，或部署目录混用了两次构建。当前构建让 Rolldown依据完整模块图自动切分 Ant Design 依赖，禁止再用 `maxSize` 强制拆开 `antd` / `@rc-component` 内部模块。请用最新的 `frontend/dist/` 完整替换旧静态目录，并确认 `index.html` 不缓存、`/assets/` 缺失时返回 `404`。不要只复制新的 `index.html` 或只覆盖部分 assets。
