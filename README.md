# RelayShelf

**面向个人与家庭自托管场景的跨设备内容中转与存储中心。**

RelayShelf 用于在 Windows、Linux、Android、iPhone 和 iPad 等设备之间，
快速保存、发送、检索和取用文本、命令、链接、图片及文件。它强调明确的
内容生命周期、可靠的大文件传输、低资源占用，以及可审计的单机生产部署。

> 当前状态：Phase 12 部署与发布工程的代码实现已合入。Phase 11 退出门已
> 通过；Phase 12 仍需在真实 Debian 13、NFSv4 NAS 和 OpenWrt nginx
> 参考环境中完成资格验证，因此 Phase 12 退出门目前仍为 **未通过**。

## 项目定位

RelayShelf 不是聊天软件、传统目录式网盘或多端文件同步盘。它围绕“内容
中转”设计：用户可以将内容暂存、长期保留、直接发送给其他用户，或在需要
时通过搜索、标签和收藏重新找到它。

主要能力包括：

- Temporary 与 Permanent 两种内容生命周期；
- 跨用户直接发送、转发、收藏、标签和回收站；
- 文本、链接、图片、附件与安全 Markdown 展示；
- 大文件分片上传、断点续传、故障恢复和服务端 SHA-256 全局去重；
- PostgreSQL 全文搜索与 `pg_trgm` 模糊匹配；
- 敏感正文加密、TOTP、会话管理、CSRF 防护和登录速率限制；
- SSE 实时事件、存储状态监控及管理端运行状态；
- 单一 Go 二进制内嵌 Web 前端，适合低功耗家庭服务器部署。

## 技术架构

- 前端：Vue 3、TypeScript、Vite、Pinia、TanStack Query、PWA；
- 后端：Go、`net/http`、chi、OpenAPI 生成接口；
- 数据库：PostgreSQL 17；
- 搜索：PostgreSQL 全文搜索与 `pg_trgm`；
- 文件存储：基于文件系统的存储适配器，生产环境由宿主机挂载 NFSv4；
- 生产运行时：Debian 13、rootful systemd Podman Quadlet；
- 外部入口：OpenWrt nginx 负责 TLS 终止与反向代理。

生产架构不以 Docker Compose、Kubernetes 或 Redis 为权威，也不会暴露
PostgreSQL 5432。详细约束参见[架构文档](docs/ARCHITECTURE.md)和
[中文生产部署指南](deploy/README.md)。

## 环境要求

项目工具版本由 `mise.toml` 固定：

| 工具 | 版本 |
| --- | --- |
| Go | 1.26.0 |
| Node.js | 24.5.0 |
| pnpm | 10.15.1 |
| golangci-lint | 2.13.1 |

此外，本地集成测试和运行需要 PostgreSQL；容器与端到端测试需要 Podman
或 Docker。推荐安装 [mise](https://mise.jdx.dev/) 统一管理工具链。

## 获取代码与安装依赖

```bash
git clone https://github.com/ZephyrLeeX/RelayShelf.git
cd RelayShelf

mise install
pnpm --dir web install --frozen-lockfile
go mod download
```

## 本地构建

`make build` 会先构建 Vue 前端，将产物复制到 Go 的内嵌资源目录，再生成
单一可执行文件 `bin/relayshelf`：

```bash
make build
./bin/relayshelf version
```

如果修改了 OpenAPI 或其他生成源，请先运行：

```bash
make generate
git diff --exit-code
```

第二条命令用于确认生成产物已经提交且可重复生成。

## 本地运行

RelayShelf 不会自动加载 `.env` 文件。请以 `.env.example` 为模板准备环境
变量，并使用独立的本地存储和暂存目录。以下示例假设 PostgreSQL 已运行在
`127.0.0.1:5432`，且已创建用户和数据库 `relayshelf`：

```bash
mkdir -p .local/storage .local/staging

export DATABASE_URL='postgres://relayshelf:CHANGE_ME@127.0.0.1:5432/relayshelf?sslmode=disable'
export STORAGE_ROOT="$PWD/.local/storage"
export STAGING_ROOT="$PWD/.local/staging"
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
export CSRF_SECRET="$(openssl rand -base64 32)"
export PUBLIC_ORIGIN='http://127.0.0.1:8080'
export LISTEN_ADDR='127.0.0.1:8080'
export STAGING_MIN_FREE_BYTES=0
export STAGING_MIN_FREE_PERCENT=0

make build
./bin/relayshelf config check
./bin/relayshelf migrate
./bin/relayshelf serve
```

启动后可检查：

```bash
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/health/ready
```

`.env.example` 中的并发数、暂存容量和安全证明项展示了完整配置。开发环境
可以降低磁盘门槛，但生产环境必须使用部署包中冻结的安全默认值，不能照搬
上述本地示例。

## 命令行入口

构建后的 RelayShelf 二进制提供以下运维命令：

| 命令 | 用途 |
| --- | --- |
| `relayshelf serve` | 启动 HTTP 服务；无参数时也是默认命令 |
| `relayshelf migrate` | 执行内嵌数据库迁移 |
| `relayshelf migrate status` | 检查数据库与当前二进制的模式兼容性 |
| `relayshelf config check` | 验证部署配置且不输出密钥值 |
| `relayshelf storage check` | 验证存储的读、写、同步、重命名和删除能力 |
| `relayshelf healthcheck` | 调用配置的 HTTP 健康检查端点 |
| `relayshelf security check` | 检查密钥备份和 TLS/代理安全证明项 |
| `relayshelf version` | 输出版本、Git 提交和构建时间 |

## 测试与质量检查

常用验证入口：

```bash
make lint           # Go 格式、go vet、golangci-lint、前端 lint/typecheck
make test           # Go/前端单元测试及 PostgreSQL 集成测试
make e2e            # Playwright 浏览器旅程
make build          # 生产形态前端与 Go 二进制构建
make container      # 构建本地容器镜像
make deploy-verify  # Quadlet、NFS 门禁、nginx 与发布策略验证
```

`make test` 会通过容器运行 PostgreSQL，默认使用 Podman。需要切换到 Docker
时可执行：

```bash
CONTAINER_RUNTIME=docker make test
```

端到端测试会启动真实 Go 服务、PostgreSQL、文件存储和浏览器。首次运行前
需要安装 Chromium：

```bash
pnpm --dir web exec playwright install --with-deps chromium
make build
make e2e
```

## 容器与生产部署

本地构建容器镜像：

```bash
make container
```

生产部署请从 [`deploy/`](deploy/README.md) 操作。部署包包含：

- RelayShelf、PostgreSQL 和私有网络 Quadlet；
- 受保护的环境变量示例；
- NFS 宿主机身份与能力门禁；
- 全新安装、升级、回滚和发布包脚本；
- OpenWrt nginx 参考配置；
- 精确镜像标签、健康依赖和发布元数据验证。

生产环境的关键边界包括 Debian 13、Podman/Quadlet >= 5.2.0、PostgreSQL
17、本地数据库磁盘、宿主机 NFSv4 挂载、非 root 应用运行时、只读根文件
系统，以及禁止使用 `latest` 镜像标签。请勿以本地开发步骤替代生产部署
流程。

## 仓库结构

```text
api/                  OpenAPI 规范
assets/brand/         品牌与图标源文件
cmd/relayshelf/       服务入口和运维命令
deploy/               生产部署包、Quadlet、脚本和 nginx 配置
docs/                 产品、架构、数据模型和资格验证文档
internal/             后端领域与平台实现
migrations/           内嵌 PostgreSQL 迁移
scripts/              生成、集成测试和资格验证脚本
sql/                  SQL 查询与生成代码
tools/                 开发和端到端测试辅助工具
web/                   Vue 前端与 Playwright 测试
```

## 文档索引

- [产品需求文档](docs/PRD.md)
- [系统架构](docs/ARCHITECTURE.md)
- [数据模型](docs/DATA_MODEL.md)
- [实施计划](docs/IMPLEMENTATION_PLAN.md)
- [Phase 11 资格验证记录](docs/PHASE11_QUALIFICATION.md)
- [Phase 12 资格验证记录](docs/PHASE12_QUALIFICATION.md)
- [生产部署指南](deploy/README.md)

## 安全提醒

- 不要提交真实 `.env`、数据库密码、`APP_ENCRYPTION_KEY` 或
  `CSRF_SECRET`；
- `APP_ENCRYPTION_KEY` 必须在虚拟机之外备份后才能确认生产安全门；
- `TRUSTED_PROXIES` 只能包含实际的 OpenWrt 代理地址或 CIDR；
- PostgreSQL 数据不得放在 NFS，5432 不得暴露到 LAN；
- NFS 缺失时不得使用空的本地目录作为降级存储；
- 发布镜像必须使用完全限定的精确 SemVer 标签，禁止 `latest`。

## 品牌资源

品牌源文件位于 `assets/brand/`，Web/PWA 图标位于 `web/public/`。
