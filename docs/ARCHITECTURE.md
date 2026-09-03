# RelayShelf — 系统架构设计

**状态：** V1 Architecture Authority 已冻结  
**版本：** 1.0-draft-authority  
**日期：** 2026-08-25

## 1. 架构目标

本架构优先优化：

- 小规模私人部署；
- Intel J4125 低功耗 CPU；
- Debian VM 6GB 内存；
- 大文件可靠生命周期；
- 低运维复杂度；
- 用户私有数据隔离；
- 后续通过可信反向代理公网暴露；
- 尽可能少的进程、容器和基础设施依赖。

系统采用：

> **Modular Monolith（模块化单体）**

除非 V1 有明确需求，否则不引入分布式系统模式。

## 2. 生产拓扑

```text
                           Internet / LAN Client
                                   │
                                   ▼
                          OpenWrt Nginx / TLS
                          Trusted Reverse Proxy
                                   │
                         HTTP / SSE / HTTP Range
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────┐
│ Debian 13 KVM VM（6 GB RAM，本地磁盘，Podman Quadlet）      │
│                                                             │
│  ┌───────────────────────────────┐                          │
│  │ relayshelf                  │                          │
│  │ Go net/http + chi             │                          │
│  │ Embedded Vue SPA              │                          │
│  │ REST / SSE / Search           │                          │
│  │ Upload / Finalize             │                          │
│  │ Thumbnail Worker              │                          │
│  │ Scheduler                     │                          │
│  └──────────────┬────────────────┘                          │
│                 │ pgxpool                                   │
│  ┌──────────────▼────────────────┐                          │
│  │ PostgreSQL 17                 │                          │
│  └───────────────────────────────┘                          │
│                                                             │
│  Local /staging                                             │
└───────────────────────┬─────────────────────────────────────┘
                        │ NFSv4 / 2.5 GbE
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ 独立物理机：飞牛 NAS / FNOS                                │
│ relayshelf/                                               │
│ ├── objects/                                                │
│ ├── derivatives/                                            │
│ └── .commit-tmp/                                            │
└─────────────────────────────────────────────────────────────┘
```

OpenWrt 与 Debian VM 位于同一台 J4125 + 8GB 物理机：

```text
OpenWrt 2GB
Debian VM 6GB
```

飞牛 NAS 是另一台独立物理设备。

## 3. 技术栈

### 3.1 Backend

- Go 1.26.x 兼容基线；
- `net/http`；
- `chi`；
- `pgx` / `pgxpool`；
- `sqlc`；
- Pure SQL Migration；
- Migration SQL 嵌入单一 Go Binary。

选择 `net/http + chi` 的原因：

- 路由层薄；
- 与标准库 Context/streaming/Range/SSE 自然兼容；
- 不把框架私有 Context 带入 Service；
- 更适合这个以 HTTP Streaming 和清晰边界为核心的小型系统。

### 3.2 Frontend

- Vue 3；
- TypeScript strict；
- Vite；
- Vue Router；
- Pinia；
- TanStack Query for Vue；
- Scoped CSS / CSS Variables；
- 少量 Headless primitives；
- 不使用 SSR / Nuxt；
- 不强制大型 UI Framework；
- 不使用 Tailwind 作为默认方案。

### 3.3 Database / Search

- PostgreSQL 17；
- `pg_trgm`；
- 不使用 Redis；
- 不使用 Message Queue；
- 不使用 Elasticsearch / Meilisearch / Typesense；
- 不使用 PgBouncer。

### 3.4 Deployment

- Podman；
- systemd Quadlet 是生产运行 Authority；
- Host 侧 rootful system Podman；
- App Container 内部仍然非 root；
- NFSv4 由 Debian Host 挂载，再 Bind Mount 给 App Container。

## 4. 进程模型

生产只有两个长期容器：

```text
relayshelf-app
relayshelf-postgres
```

Go Binary：

```text
relayshelf serve
relayshelf migrate
relayshelf storage check
relayshelf version
```

V1 不存在：

- frontend container；
- worker container；
- scheduler container；
- Redis；
- search container；
- MinIO。

## 5. 后端模块

```text
internal/
├── auth/
├── users/
├── messages/
├── tags/
├── files/
├── uploads/
├── search/
├── realtime/
├── audit/
├── settings/
├── admin/
├── jobs/
├── storage/
└── platform/
```

### 5.1 模块职责

**auth**  
Login、Logout、Password Verify、Session、Device、CSRF、Rate Limit、未来 MFA 边界。

**users**  
User Identity、Status、Admin Role、账号级管理。

**messages**  
Message Lifecycle、编辑、Sensitive Toggle、Direct Send、Forward、Favorite、Optimistic Concurrency、Trash/Restore/Purge 编排。

**tags**  
User-private Tag CRUD、Normalization、Color、MessageTag。

**files**  
FileObject、MessageAttachment、FileDerivative、引用生命周期、Attachment-authorized Download。

**uploads**  
UploadSession、Parts、本地 Staging、断点续传、Complete / Finalize orchestration、Upload Expiration。

**search**  
只读搜索模块，负责 pg_trgm 和 Filter，不持有独立搜索数据库。

**realtime**  
内存 SSE Hub，只负责事件通知。

**audit**  
Append-only Security Audit。

**settings**  
Runtime DB Settings。

**admin**  
薄编排层。不能绕过 users/settings/audit/files/messages Service。

**jobs**  
Persistent Background Jobs + Scheduler。不能成为业务规则 Owner。

**storage**  
文件字节存储抽象，不理解 Message / User 业务。

**platform**  
只放真正共用基础设施：Config、DB、HTTP Helpers、Logging、Trace、Crypto、Clock、ID。

## 6. 依赖规则

标准路径：

```text
HTTP Handler
    ↓
Domain Service
    ↓
Repository / Provider
```

硬性规则：

- Handler 禁止直接 SQL。
- Handler 禁止直接访问文件系统。
- Repository 不知道 HTTP。
- Storage 不知道 Message Ownership。
- Search 只读。
- Realtime 绝不是 Source of Truth。
- Admin 不允许绕过 Domain Service。
- 写操作必须由拥有该 Invariant 的 Module 执行。
- 高效 Read Model 可以跨表 Join。
- OpenAPI Generated Type 只存在 HTTP Boundary。
- sqlc Generated Type 只存在 Repository Boundary。

模块内部保持扁平，例如：

```text
internal/messages/
├── model.go
├── service.go
├── repository.go
├── handler.go
├── errors.go
└── events.go
```

不建立重型 DDD 五层目录。

## 7. API 架构

### 7.1 Contract First

Authority：

```text
api/openapi.yaml
```

生成：

```text
OpenAPI
├── Go HTTP Boundary Types / Interfaces
└── TypeScript API Client / Types
```

Generated Code：

- 提交 Git；
- 禁止手工改；
- CI 重新 Generate 后 `git diff --exit-code`。

### 7.2 API 约定

- `/api/v1`；
- JSON camelCase；
- DB snake_case；
- UUIDv7，对外 string；
- UTC / RFC3339；
- 成功直接 Resource；
- Error 固定：

```json
{
  "code": "MESSAGE_VERSION_CONFLICT",
  "message": "内容已在其他设备修改",
  "traceId": "...",
  "details": null
}
```

前端只根据 `code` 做逻辑判断。

### 7.3 Pagination

```json
{
  "items": [],
  "nextCursor": "opaque-or-null"
}
```

- Default = 30；
- Max = 100；
- 不默认计算 totalCount；
- Cursor 对 Client opaque。

### 7.4 Optimistic Concurrency

Message：

```text
version BIGINT
expectedVersion
```

Stale Write：

```text
HTTP 409
MESSAGE_VERSION_CONFLICT
```

### 7.5 Action Endpoint

业务 Command 不强塞进 PATCH：

```text
POST /messages/{id}/make-permanent
POST /messages/{id}/trash
POST /trash/{id}/restore
DELETE /trash/{id}
POST /messages/{id}/forward
```

## 8. Authentication

V1：Opaque Server-side Session，不用 JWT Login Session。

```text
Browser Raw Random Token
        │
        ▼
SHA-256(token)
        │
        ▼
sessions.token_hash
```

Raw Token：

- CSPRNG 32 bytes；
- 无 user ID；
- 无 role；
- 无 expiration metadata。

Session：

```text
idle 30 days
absolute 90 days
```

`last_seen_at` 必须节流，不允许每个 HTTP 请求都 UPDATE。

Password：Argon2id，参数按真实 J4125 标定。

## 9. CSRF / Origin / Proxy / Host

生产是 Same-Origin SPA/API。

规则：

- 默认不开 CORS。
- Cookie-auth 写请求检查 Origin / Referer。
- 同时验证 Session-bound CSRF Token。
- Forwarded Header 只信任 `TRUSTED_PROXIES`。
- `PUBLIC_ORIGIN` 是 Public Scheme/Host Authority。
- Host / Forwarded Host 不匹配时拒绝。
- 公网 HTTPS 强制 Secure Cookie。
- HSTS 由真正终止 TLS 的 OpenWrt Nginx 设置。

## 10. Security Headers 与不可信附件

Go App 负责：

- Content-Security-Policy；
- X-Content-Type-Options；
- Referrer-Policy；
- Permissions-Policy。

CSP 尽量接近：

```text
default-src 'self'
```

避免 `unsafe-inline` / `unsafe-eval`。

HTML / SVG / XML 等 Active Content 禁止在主站 Origin 直接执行。

Client MIME 只做参考；安全判断以 Server-detected MIME 为 Authority。

## 11. Sensitive Body 架构

Message 两种互斥存储状态：

普通：

```text
sensitive=false
body_plaintext != NULL
body_ciphertext = NULL
```

敏感：

```text
sensitive=true
body_plaintext = NULL
body_ciphertext != NULL
body_nonce != NULL
body_encryption_version != NULL
```

加密：

```text
AES-256-GCM
Key   = APP_ENCRYPTION_KEY
Nonce = Fresh Random
AAD   = version || messageId || ownerId
```

普通 Message List/Detail 不自动返回 Sensitive Plaintext。

Sensitive Body 通过独立 Endpoint 显式获取，并返回 `Cache-Control: no-store`。

## 12. Search 架构

因为实际内容包含：

- 中文；
- Code；
- Shell；
- URL；
- Hostname；
- IP；
- 文件名；
- API Path；

V1 以 PostgreSQL `pg_trgm` 为核心，而不是语言型 FTS Parser。

搜索 Authority：

```text
messages.body_plaintext
message_attachments.original_filename
tags / metadata joins
```

没有 `search_documents` Shadow Table。

敏感正文因为 `body_plaintext=NULL` 天然不进明文索引。

Search SQL 必须先约束：

```text
messages.owner_id = current_user
```

不能搜索完再在 Go Filter Owner。

多 Token AND。

2 字符允许，但性能 Best Effort；正常 3+ 字符才进入正式性能目标。

## 13. Upload 架构

### 13.1 Client API

```text
POST /uploads
PUT /uploads/{id}/parts/{partNumber}
GET /uploads/{id}
POST /uploads/{id}/complete
```

Server 返回 Chunk Size，Client 不能自己选。

默认：8 MiB。

Browser：

```text
per-file concurrent chunks = 2
global concurrent chunks   = 4
```

不计算 Full-file SHA-256。

### 13.2 Local Staging

每个 UploadSession 对应 VM 本地一个逻辑/稀疏文件。

Part Upload：

- Raw `application/octet-stream`；
- `io.LimitedReader` / Streaming Copy；
- 禁止 `io.ReadAll`；
- `WriteAt(offset)`；
- 只有 Body Size 精确正确才记录 Part Completion；
- 相同 `(upload,part)` 允许幂等覆盖。

Server Active Chunk Write 上限默认 8。

### 13.3 Complete Lock

Complete 对 UploadSession：

```text
SELECT ... FOR UPDATE
```

状态：

```text
UPLOADING → COMPLETING → COMPLETED
```

哈希前先严格检查 Part 完整性与总大小。

## 14. File Finalize 架构

最终流程：

```text
UploadSession COMPLETING
        ↓
Sequential Read VM Staging
        ↓
SHA-256
        ↓
Lookup READY FileObject by (sha256,size)
   ├── Found
   │     ↓
   │  Reuse FileObject
   │  Delete Staging
   │
   └── Not Found
         ↓
      INSERT FileObject PENDING
         ↓
      Stream Staging → NAS .commit-tmp/<fileObjectId>
         ↓
      fsync
         ↓
      Atomic Rename within NAS filesystem
         ↓
      FileObject READY
         ↓
      UploadSession COMPLETED
         ↓
      Delete Staging
```

`.commit-tmp` 与 `objects` 必须在 NAS 同一 FileSystem / Export，以保证最终 Rename 原子性。

默认：

```text
FILE_FINALIZE_CONCURRENCY=1
```

## 15. Dedup 与 Crash Recovery

Dedup Authority：

```text
UNIQUE(sha256,size)
```

V1：

- Single App Instance；
- J4125 默认 Finalize=1；
- 不做 Distributed Lock。

遇到同 Hash 但 FileObject 正处于 `PENDING` / `DELETING`：

- 返回 Retryable Error；
- 交给 Reconciliation / Delete 完成；
- 不做复杂 Takeover。

### 15.1 Reconciliation

必须能够处理：

```text
PENDING + final file exists
PENDING + no final file
DELETING + file exists
DELETING + file already absent
```

不能假定 PostgreSQL + NFS 原子提交。

## 16. Upload Single Consumption

Completed UploadSession 在创建 Message 成功后写：

```text
consumed_at
consumed_message_id
```

一次普通 Upload 只能被一个 Message Creation 消费。

HTTP Retry 通过 Idempotency-Key 返回第一次结果，不重复消费。

## 17. Idempotency

持久化表：

```text
(user_id,operation,key)
```

约 24 小时。

记录 Request Hash 和安全结果元数据。

规则：

- Same Key + Same Request → 返回第一次结果；
- Same Key + Different Request → `IDEMPOTENCY_KEY_REUSED`。

适用：

- Message Create；
- Direct Send；
- Forward。

## 18. Message + Attachment 事务边界

当所有 Upload：

```text
COMPLETED
FileObject READY
```

后，在单个 PostgreSQL Transaction 创建：

- Message；
- MessageAttachments；
- MessageTags；
- Upload consumed markers；
- 必须的 BackgroundJob；
- Idempotency Result。

如果事务失败：

- 不存在 Message；
- READY 但 0 引用的 FileObject 进入 orphan grace，后续 GC。

## 19. StorageProvider

V1 命名：

> `FilesystemStorageAdapter`

它只知道 `/storage` 是一个满足 Contract 的文件系统，不知道实际背后是：

- ext4；
- XFS；
- NFS mount。

能力：

- Create / Write Temp；
- Sequential Read；
- Stat；
- Range / Seek；
- fsync；
- Atomic Commit / Rename；
- Delete。

未来可新增：

```text
S3StorageAdapter
```

## 20. NFS 架构

NFSv4 由 Debian Host 挂载，再 Bind Mount 到 App Container。

App Container：

- 不自己 Mount NFS；
- 不要 privileged；
- 不需要额外 Mount Capability。

### 20.1 Cold Start

必须：

```text
network-online
→ NFS mount
→ relayshelf-app Quadlet
```

如果 NAS 启动时不可用，App 默认不启动。

原因：避免 `/mnt/relayshelf` 未 Mount 时，应用误把正式文件写进 VM 本地空目录。

### 20.2 Runtime NFS Outage

使用可靠 hard mount 语义意味着已有 NFS syscall 可能在故障期间阻塞。

因此架构不假装所有 NFS 调用都能立即 Timeout。

防护：

- 有限 Storage Operation Semaphore；
- Storage Health → degraded；
- 已知 degraded 后新 Storage-heavy 请求快速拒绝；
- DB/Text/Tag 等不依赖 Storage 的请求可继续；
- Upload Chunk 仍可继续写 VM Staging；
- Complete 在 Storage 恢复后 Retry。

应用的 `storage.Monitor` 是运行时健康状态的单一权威来源：它以有界、去重的
后台探针更新内存快照；普通登录用户的 storage status API 只读取该快照，不在
HTTP 请求中触碰 hard NFS。Web 用它显示全局降级提示、阻止新附件进入 staging，
同时保留已有可恢复上传并继续提供纯文本能力。

NAS 重启或重新导出造成的 `ESTALE` 由 Debian Host 的 systemd timer 负责恢复。
只有轻量探针连续两次明确返回 `Stale file handle`，且挂载类型和 fstab source
匹配时，宿主机才停止应用并执行有界 remount、UID 65532 实际读写验证和容器内
storage check。容器不获得 mount 权限，API/Web 也不能触发 recovery；网络超时、
权限、容量及其他通用错误不进入 destructive recovery。

## 21. Download 架构

Download 以 Attachment 为授权边界：

```text
Client
→ Go Session Auth
→ Attachment
→ Message.owner check
→ FileObject
→ FilesystemStorage
→ Range Response
```

Go 负责：

- Range；
- Content-Length；
- ETag；
- Last-Modified；
- If-Range / If-None-Match；
- Content-Disposition；
- Cancellation。

SHA-256 可以内部用于 Stable ETag，但普通 Metadata API 不公开 Global Hash。

V1 不用 Nginx `alias` 直读 NAS，也不使用 X-Accel-Redirect。

## 22. SSE 架构

内存结构：

```text
map[userID][]subscriber
```

事件示例：

```json
{
  "id": "...",
  "type": "message.updated",
  "resourceId": "...",
  "version": 5,
  "originDeviceId": "...",
  "occurredAt": "..."
}
```

不包含正文或敏感数据。

事件在 DB Commit 后 Publish。

接受：

```text
DB Commit 成功
→ 进程恰好 Crash
→ 少一次 SSE UI Hint
```

因为 DB 才是 Truth，重新刷新即可恢复。

不引入 Outbox。

Reconnect / Page Visible 后 Selective Query Invalidate。

## 23. Background Jobs 与 Scheduler

### 23.1 Persistent Jobs

用于必须跨重启可靠完成的异步任务。

初期主要：Thumbnail。

状态：

```text
PENDING
RUNNING
COMPLETED
FAILED
```

包含 attempts、next_run_at、Safe Error 等。

Stuck RUNNING 超时后可恢复到 Retry 状态。

Worker：

- Commit 后内存 wake；
- 30–60 秒 Safety Poll；
- Thumbnail Worker 默认 1。

### 23.2 Scheduler

大约 Hourly。

PostgreSQL Advisory Lock 防止误启动双 App 时重复 Sweep。

每批约 100–500 行，短事务。

## 24. FileDerivative

Derivative 是 FileObject-level：

```text
FileObject
└── FileDerivative(kind=THUMBNAIL_SMALL)
```

```text
UNIQUE(source_file_id,kind)
```

相同物理图片被多个 Message 引用时只生成一次。

Derivative 的用户访问仍必须从有权限的 Attachment/Message 路径鉴权。

## 25. Frontend 架构

```text
web/src/
├── app/
├── features/
│   ├── auth/
│   ├── messages/
│   ├── uploads/
│   ├── tags/
│   ├── search/
│   ├── trash/
│   ├── sessions/
│   └── admin/
├── shared/
│   ├── api/
│   ├── ui/
│   ├── utils/
│   └── types/
└── main.ts
```

原则：按 Feature 划分，不按 `components/views/services` 大桶划分。

Pinia：

- 当前 User；
- 当前 Device；
- UI Preference；
- Active Upload metadata；
- Client-only global state。

TanStack Query：

- Messages；
- Tags；
- Search；
- Server Cache；
- Mutation Invalidate。

普通 API 统一通过 Generated Client。

例外专门 Transport：

- SSE；
- Chunk Upload；
- File Download。

## 26. UploadManager

UploadManager 独立于 MessageComposer。

状态：

```text
QUEUED
CREATING
UPLOADING
PAUSED
COMPLETING
COMPLETED
FAILED
EXPIRED
CANCELED
```

负责：

- Create UploadSession；
- Part Scheduling；
- Per-file / Global Semaphore；
- Retry；
- Resume；
- Complete；
- Cancel；
- Progress。

不引入 XState，使用 TypeScript Discriminated Union / Reducer 即可。

UploadManager 是 App-level Singleton，但不把 File bytes 放进 Pinia。

## 27. PWA 架构

Production Vue Build 最终嵌入 Go Binary。

Service Worker 只缓存 App Shell。

Private API / Attachment Network Only。

PWA Update 不能强制打断 Active Upload。

## 28. Static Assets

生产构建：

```text
pnpm build
→ web/dist
→ copy internal/webui/dist
→ go:embed
→ single Go binary
```

Routing：

```text
/api/*      API only
/assets/*   Embedded Vite Hash Assets
其他 SPA Route → index.html
```

Hashed Asset：一年 immutable Cache。

`index.html`：短缓存 / no-cache。

## 29. PostgreSQL Connection

初始：

```text
pgxpool MinConns=1
pgxpool MaxConns=10
```

PostgreSQL PGDATA 在 Debian VM 本地盘，禁止放 NFS。

6GB VM 下数据库 Memory 保守控制在约 512MB–1GB 级，后续真实 Benchmark 再调整。

## 30. Podman Quadlet

生产 Deployment Authority：Quadlet，不是 Compose。

建议：

```text
deploy/
├── quadlet/
│   ├── relayshelf-app.container
│   ├── relayshelf-postgres.container
│   └── relayshelf.network
├── env/
│   └── relayshelf.env.example
└── scripts/
    ├── install.sh
    ├── upgrade.sh
    └── storage-check.sh
```

Rootful Unit 安装到：

```text
/etc/containers/systemd/
```

App Container 内仍使用 non-root UID。

## 31. OpenWrt Nginx

OpenWrt 负责 TLS。

必须：

- Debian VM 固定 IP；
- Host / X-Forwarded-For / X-Forwarded-Proto；
- `/api/v1/*` 禁止 Proxy Cache；
- SSE `proxy_buffering off`；
- Download `proxy_buffering off`；
- Chunk Upload `proxy_request_buffering off`；
- `client_max_body_size` 按 Chunk 设置，例如 16–32MiB，而不是 2GB。

原因：OpenWrt 自身只有 2GB RAM，还同时承担 KVM/路由工作，不应为文件流量制造额外 Buffer 压力。

## 32. Config 模型

### 32.1 Deployment Environment

例如：

```text
DATABASE_URL
STORAGE_ROOT=/storage
STAGING_ROOT=/staging
APP_ENCRYPTION_KEY
CSRF_SECRET
PUBLIC_ORIGIN
TRUSTED_PROXIES
FILE_FINALIZE_CONCURRENCY=1
THUMBNAIL_WORKERS=1
MAX_ACTIVE_CHUNK_WRITES=8
UPLOAD_STAGING_MAX_BYTES=34359738368
```

启动时读取一次并严格校验。

不用 Viper 等多来源配置框架。

Secret：

- 无危险 Default；
- 不允许 String()/Log 输出明文。

### 32.2 Runtime Settings

PostgreSQL：

- Temporary TTL；
- Trash TTL；
- Max File Size；
- Max Logical Storage；
- Audit Retention；
- Upload Retention。

## 33. Health

```text
/health/live
/health/ready
```

Podman Container Healthcheck 更偏向 `live`，避免短暂 DB/NFS 故障造成 Container Restart Storm。

Admin / Monitoring 使用 `ready` 观察 Dependency 状态。

## 34. Startup / Shutdown

### Startup

1. Load + Validate Config；
2. Connect PostgreSQL；
3. Verify Schema Compatibility；
4. Verify NFS Mount / Storage Capability；
5. Init StorageProvider；
6. Bounded Reconciliation；
7. Start Workers；
8. Start Scheduler；
9. Start HTTP Server。

以下情况 Fail Fast：

- Secret 缺失；
- Config 非法；
- DB Schema 不兼容；
- Cold Start 时 Storage/NFS 不存在。

### Graceful Shutdown

- 停止接收新请求；
- 关闭 SSE；
- 停止 Scheduler/Worker Intake；
- 最多约 30 秒等待短任务；
- 关闭 DB Pool。

正在 Upload 的 Chunk 中断后可断点续传，不需要为了 2GB 文件无限阻止关机。

## 35. Migration

Migration：Pure PostgreSQL SQL，Embedded Binary。

生产显式：

```text
relayshelf migrate
```

App Boot 不自动偷偷改 Schema。

家庭系统不要求 Zero-downtime Upgrade。

Migration 失败立即停止 Upgrade。

不支持自动 Schema Downgrade。

App 启动发现 DB Schema 比自身更新且不兼容时拒绝启动。

## 36. Version / Release

- App 使用 SemVer；
- API `/api/v1` 只有 Breaking Contract 才升级 `/api/v2`；
- Binary 注入 Version / GitCommit / BuildTime；
- Production Image Pin Exact Version；
- 禁止生产使用 `latest`；
- CI 可以构建 `linux/amd64` + `linux/arm64`；
- Release 提供小型 Deploy Bundle，不要求用户 Clone 全源码部署。

## 37. Resource Budget

Reference Defaults：

```text
FILE_FINALIZE_CONCURRENCY=1
THUMBNAIL_WORKERS=1
MAX_ACTIVE_CHUNK_WRITES=8
browser global chunks=4
pgx MaxConns=10
```

目标：

- Go App Idle CPU 接近 0；
- Go App Idle RSS 尽量 < 150MiB；
- 无重任务争抢时 LAN 普通 API P95 < 200ms；
- 100k Message 普通 3+ 字符搜索 P95 < 500ms；
- 不以跑满 2.5GbE 为目标；
- 稳定、可恢复、不给 OpenWrt/NAS 制造资源灾难优先于峰值吞吐。

## 38. Architecture Invariants

以下是实现不可违反的硬约束：

1. User Attachment 必须通过 Message Ownership 鉴权；FileObject 不能作为用户直接资源。
2. 任何 MessageAttachment 引用存在时禁止物理删除 FileObject，Trash 引用也算。
3. `sensitive=true` 时禁止数据库保留 `body_plaintext`。
4. Sensitive Ciphertext 绝不能作为可搜索明文处理。
5. Direct Send / Forward 创建新的独立 Message；Sensitive 正文必须重新加密。
6. Upload Complete 成功前对应 FileObject 必须 READY。
7. Message Attachment 只能绑定 READY FileObject。
8. Private Data SQL 不能绕开 Owner Check。
9. SSE 不能成为业务 Truth。
10. PostgreSQL + NFS 之间通过 State Machine + Reconciliation 解决 Crash Window，不能假设跨系统事务。
11. Encryption Key 等 Secret 在 DB 外部，已有数据时 Secret 缺失必须启动失败，禁止静默重建。
12. Cold Start 时禁止写进未挂载的 NFS Mountpoint。
13. Finalize、Thumbnail、Chunk Write 等重任务并发必须有上限。
14. Search 必须在 SQL Owner Scope 内执行，不允许 Go 事后过滤。
15. OpenAPI/sqlc Generated Types 不得侵入 Domain Service API。
