# ascli 分享下载客户端

## 可行性分析

结论：可以实现，沿用现有分享鉴权和下载 API，不需要新增服务器文件代理。

- `GET /api/v1/public/shares/{token}/` 获取分享状态，已过期或撤销的分享直接失败。
- 密码分享通过 `POST .../verify` 换取 30 分钟的 Bearer grant；公开分享无需密码。错误密码和限流响应直接失败，不自动重试验证。
- `GET .../nodes` 支持批量目标和按 `parent_id` 遍历目录，可保留目录结构下载。
- `POST .../nodes/{id}/download` 的 `delivery=external` 返回 S3/OSS 预签名 URL，客户端直接 GET 对象存储，不消耗 AltasCI 文件传输带宽；`delivery=local` 则携带 grant 请求本地内容 API。
- 已有预签名 URL 本身就是短期访问凭证，可以直接下载，无须再次向 AltasCI 验证密码。客户端不能延长其有效期，过期时需重新生成。
- Web 与 API 独立部署，无法只从 Web 域名推断 API 域名。因此前端构建生成 `/.well-known/altasci.json`（`api_url` 与前端配置一致），CLI 据此发现 API；旧部署使用 `--api-url` 或 `ASCLI_API_URL`。
- Go 客户端仅依赖标准库，可以关闭 CGO 独立构建，适合 CI 容器和跨平台分发。CI 无需终端输入，通过参数或环境变量提供密码，任一下载失败立即非零退出。

大文件采用流式写盘，内存不随文件大小增长。文件完成前写入同目录临时文件；传输失败清理临时文件，默认不覆盖已有文件。目录分享不是事务快照：出错前已完成的文件会保留，分享内容变动或 grant 到期会中止，重新执行需自行处理已存在文件。

## 构建与使用

在仓库根目录运行（只需 Go 1.27+，无需 CGO、Node.js 或服务器配置）：

```bash
make build-cli
./dist/ascli --version
# 可选：安装到 PATH
install -m 755 ./dist/ascli "$HOME/.local/bin/ascli"
```

现有 `v*` 标签 Release 工作流会测试 CLI 与公开分享集成，并附带 Linux/macOS/Windows 的 amd64、arm64 二进制及 `ascli-checksums.txt`。下载对应平台文件后校验 SHA-256，重命名为 `ascli`（Windows 为 `ascli.exe`），放入 PATH；Linux/macOS 还需 `chmod +x`。

CLI 与服务端统一使用 GitHub Release 标签注入版本号。例如标签 `v1.2.3` 对应 `ascli --version` 输出 `ascli v1.2.3`，服务端输出 `altasci-server v1.2.3`。发布前会运行这两个命令检查版本一致性，并检查全部八个二进制非空；`checksums.txt` 覆盖服务端和 CLI 的全部二进制，`ascli-checksums.txt` 仅覆盖六个 CLI 二进制。缺失产物、版本不匹配或校验失败都会阻止发布。

本地指定同一版本构建：`make build-backend build-cli VERSION=v1.2.3`；直接使用 `go run` 时版本为 `dev`。

GitHub Actions 还会保留名为 `altasci-storage-<版本标签>` 的构建 Artifact，内容与 Release 附件一致；即使后续 Release 上传失败，也可从对应运行下载已经验证的产物。

流水线先创建草稿 Release，全部附件上传成功后才公开发布。已存在同名 Release（含草稿）时停止，不自动覆盖；上传失败后可检查并补齐草稿再发布，已发布版本的修复使用新标签。

```bash
# 单个文件，默认下载到当前工作目录
ascli 'https://web.example.com/s/01234-56789-01234-56789-01234-56789' --pwd 1234

# 自动创建目标目录；目录/多文件分享会下载全部内容并保留目录名
ascli 'https://web.example.com/s/TOKEN' --pwd 1234 --download-dir ./artifacts

# “携带密码”链接；公开分享不需要密码
ascli 'https://web.example.com/s/TOKEN?code=1234'

# 直接使用预签名 URL，不请求 AltasCI，不需要 --pwd
ascli 'https://bucket.example.com/build.tar.gz?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=...' --download-dir ./artifacts

# 旧前端尚未发布发现文件，或需要覆盖 API 地址
ascli 'https://web.example.com/s/TOKEN' --pwd 1234 --api-url https://api.example.com
```

支持 Web `/s/{token}` 链接，也支持 API `/api/v1/public/shares/{token}/` 元数据链接；后者默认直接使用该链接的 API Origin。预签名 URL 按签名查询字段识别，原样保留查询串，支持 S3 SigV4、旧版 S3 签名和 OSS 签名。请给 URL 加引号，避免 shell 将 `&` 当成命令分隔符。

密码优先级：`--pwd` → `ASCLI_PWD` → 分享链接 `?code=`。API 地址优先级：`--api-url` → `ASCLI_API_URL` → API 元数据链接 Origin / Web 发现文件。未提供必需密码会立即失败，不进行交互输入。生产地址要求 HTTPS，开发测试允许 loopback HTTP。

## CI/CD

把密码放入 CI 的 Secret，并映射到 `ASCLI_PWD`。客户端输出不包含密码、完整分享链接、grant 或预签名查询串；日志会包含下载文件的相对名称及字节数。

```bash
#!/usr/bin/env bash
set -euo pipefail
export ASCLI_PWD="${ALTASCI_SHARE_PASSWORD:?missing CI secret}"
ascli "${ALTASCI_SHARE_URL:?missing share URL}" \
  --download-dir ./artifacts \
  --log-format json \
  --timeout 1h \
  --idle-timeout 30s | tee ascli.log
# ascli 或 tee 失败时，pipefail + set -e 会阻止执行此步骤
./consume-artifacts.sh
```

所有日志（包括错误）写入 STDOUT，`--log-format json` 输出每行一个 JSON 对象。日志不输出终端动画，适合重定向。每个文件有开始、完成日志，任一错误中止后续下载。shell/流水线必须检查进程退出码；通过 `tee` 等管道执行时启用 `pipefail`，不要用 `|| true` 吞掉错误。

| 退出码 | 含义 |
| --- | --- |
| 0 | 全部下载成功，或显示帮助/版本 |
| 1 | 网络、TLS、超时、协议、传输或文件系统失败，或目标文件已存在 |
| 2 | 参数、必需密码或 API 发现配置错误 |
| 3 | HTTP 401/403/404/410/429：权限拒绝、资源不存在、分享失效或限流 |
| 130 | 收到中断/终止信号 |

`--timeout` 默认 `30m`，限制单次 HTTP 请求（含响应体）的总时长；`--idle-timeout` 默认 `30s`，限制连接、响应头以及响应体停滞时间。大文件需要根据网络速度调整总超时。错误密码、HTTP 错误和网络故障均不自动重试，避免反复验证触发封禁或掩盖失败。

## 文件与存储行为

- 单文件直接保存到下载目录；单目录分享保留该目录名，批量分享保留各目标名，空目录也会创建。重叠选择按节点去重。
- 下载前检查整个文件清单，拒绝路径穿越、保留设备名和大小写冲突；文件系统操作受下载目录约束，不能沿符号链接写到目录之外。
- 默认拒绝已有文件；`--overwrite` 允许成功下载后替换普通文件。目录和符号链接不会作为文件被覆盖。目录分享中，错误前已完成的文件保留。
- 临时文件位于目标文件同目录，权限 `0600`。检查响应长度和分享元数据大小、同步文件后才发布最终文件；出错会清理临时文件，保留已有文件。默认发布使用同目录硬链接原子防覆盖，因此目标文件系统须支持硬链接；不支持时返回错误，可在确认允许覆盖后用 `--overwrite` 的重命名方式。强制杀死进程（SIGKILL）或断电可能留下 `.ascli-*.part` 文件。
- 原始预签名 URL 优先使用 `Content-Disposition`（支持 UTF-8）命名，其次使用签名中的 `response-content-disposition`，最后使用 URL 路径末段。
- S3/OSS 下载只向 AltasCI 请求鉴权、元数据和签名，文件内容直接来自对象存储；对象存储请求不携带 AltasCI grant 或 Cookie。本地内容只向原 API 内容地址发送 grant，并拒绝携带 grant 的重定向。
- 预签名 URL 到期、分享失效或 30 分钟 grant 到期后需要重新执行。当前不支持断点续传、并发下载或内容哈希校验；长度检查不能替代完整性哈希。

实现依据：[Go 文件系统目录约束](https://pkg.go.dev/os#Root)、[Vite 插件钩子](https://vite.dev/guide/api-plugin.html)。
