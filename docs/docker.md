# Docker 后端部署

镜像只包含 Go 后端，不构建或托管前端。需要 Docker Engine 和 Docker Compose；宿主机无需安装 Go 或 C 编译器。以下命令均在仓库根目录执行。

## 构建和首次初始化

```bash
docker compose build backend
docker compose run --rm backend init \
  --admin-email admin@example.com \
  --public-web-url https://web.example.com \
  --public-api-url https://api.example.com
docker compose up -d backend
docker compose ps
curl --fail http://127.0.0.1:8080/health/ready
```

将示例域名替换成实际 HTTPS Origin。`init` 在交互式终端中读取两次管理员密码，不回显；不要加 `-T`，也不支持通过环境变量传入密码。初始化只执行一次，创建数据库、主密钥和管理员；后续直接 `up -d`。未初始化时，服务启动会报错，不会自动创建默认账号。

Compose 默认将端口映射到宿主机 `127.0.0.1:8080`，由宿主机反向代理提供 HTTPS。可用 `BACKEND_PORT=18080 docker compose up -d backend` 更换宿主端口。容器内监听 `0.0.0.0:8080`，健康检查访问 `/health/ready`。

## 持久化和权限

Compose 命名卷 `backend-data` 挂载到 `/var/lib/altasci`，实际卷名包含 Compose 项目前缀：

| 容器路径 | 内容 |
| --- | --- |
| `/var/lib/altasci/altasci-network-storage.db` | SQLite 数据库及同目录 WAL/SHM |
| `/var/lib/altasci/master.key` | 主密钥，初始化时权限为 `0600` |
| `/var/lib/altasci/objects` | 推荐的 Local Storage 根目录 |

首次登录后在存储管理中创建 Local 后端，将 `root_path` 设为 `/var/lib/altasci/objects`。应用会在根目录下创建对象与上传临时目录，均保存在持久卷中。S3/OSS 配置仍通过管理界面保存。

镜像以 UID/GID `10001:10001` 运行。新命名卷会继承镜像目录权限；如果改成宿主目录绑定挂载，需提前创建目录并赋予该 UID/GID 读写权限。已有数据迁移时，停止原服务，将数据库、主密钥和本地对象一起恢复到对应路径，并更新数据库中的 Local Storage `root_path`。同一 SQLite 数据目录只运行一个后端实例。

容器根文件系统只读，`/tmp` 使用临时文件系统；重建容器不会删除命名卷。`docker compose down` 保留数据，`docker compose down -v` 会删除数据卷，不能用于常规升级。

## 配置和运维

镜像自带 `/app/config.toml`，来源为 `backend/docker/config.toml`。业务设置保存在 SQLite 中；bootstrap TOML 仅定义监听地址、数据库和主密钥路径。需要覆盖时，可在 Compose 的 `volumes` 下添加只读挂载：

```yaml
      - ./config.container.toml:/app/config.toml:ro
```

自定义配置文件须允许 UID 10001 读取，并保持容器监听 `0.0.0.0:8080`；如修改容器端口，需同时修改端口映射和健康检查。

```bash
# 日志、版本和密码重置
docker compose logs -f backend
docker compose run --rm backend version
docker compose exec backend altasci-server admin-reset-password

# 升级：先备份，再构建并重建容器
docker compose build backend
docker compose up -d --no-deps backend
```

启动时自动应用数据库迁移。备份和恢复遵循[部署文档](deployment.md#备份)，需同时保护 SQLite、主密钥和本地对象。停止容器后可复制完整数据目录；在线备份应使用 SQLite Backup API，不能只复制活动中的 `.db` 文件。

后端直接作为容器主进程接收 SIGTERM；Compose 预留 20 秒停止时间，覆盖应用的 15 秒优雅关闭窗口。配置可信代理时，应使用容器实际看到的 TCP peer IP/CIDR，不能直接沿用宿主机部署的 `127.0.0.1/32`。

## 不使用 Compose

```bash
docker build --build-arg VERSION=v0.1.3 -t altasci-storage-backend:local ./backend
docker volume create altasci-backend-data
docker run --rm -it -v altasci-backend-data:/var/lib/altasci \
  altasci-storage-backend:local init \
  --admin-email admin@example.com \
  --public-web-url https://web.example.com \
  --public-api-url https://api.example.com
docker run -d --name altasci-backend --restart unless-stopped \
  --read-only --tmpfs /tmp:rw,noexec,nosuid,size=64m,mode=1777 \
  --cap-drop ALL --security-opt no-new-privileges:true --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 -v altasci-backend-data:/var/lib/altasci \
  altasci-storage-backend:local
```

构建保留 CGO 支持，并在 Debian 构建与运行环境之间保持 glibc 兼容。可通过 `docker buildx build --platform linux/amd64,linux/arm64 ... ./backend` 构建多架构镜像，前提是构建器具备对应原生节点或 QEMU 支持。构建上下文使用白名单，不包含现有配置、数据库、主密钥或前端文件。镜像没有自动推送到镜像仓库；如需发布，使用自己的仓库地址标记并推送。
