# RelayShelf — 实施计划

**状态：** V1 Execution Authority  
**版本：** 1.0-draft-authority  
**日期：** 2026-08-25

## 1. 实施原则

本项目按**数据完整性和生命周期风险**排序开发，而不是按“页面看起来完成”排序。

不得先做一个完整 Vue 原型，再倒推后端和文件生命周期。

最高风险区域：

- Database Constraints；
- Auth / Session；
- Chunk Upload / Resume；
- SHA-256 Dedup；
- PostgreSQL / NFS Crash Window；
- File Reference GC；
- Sensitive Encryption；
- User-isolated Search；
- Cross-device Consistency。

每个 Phase 都有 Exit Gate。未通过 Gate 的上游能力不能被下游当作稳定基础。

## 2. 全局开发规则

1. OpenAPI 是 HTTP Contract Authority。
2. SQL Migration 是 DB Schema Authority。
3. Repository/Integration Test 必须使用真实 PostgreSQL，不用 SQLite 替代。
4. V1 不引入 Redis、MQ、Elasticsearch、MinIO、独立 Worker Process。
5. Generated OpenAPI/sqlc 代码禁止手工修改。
6. File Lifecycle 必须有 Failure Injection Test。
7. 物理文件 Delete 只能根据真实 Attachment References 判断。
8. 私人 Message SQL 必须 Owner-scoped。
9. 重 CPU/IO Work 必须有显式并发上限。
10. Production Target：Debian VM Podman Quadlet + PostgreSQL Local Disk + NFSv4 NAS。

## 3. Phase 总览

```text
P0  Repository / Toolchain / CI
 ↓
P1  PostgreSQL Schema / Migration / Config
 ↓
P2  Auth / Users / Sessions / CSRF
 ↓
P3  Messages / Tags / Lifecycle / Sensitive Body
 ↓
P4  UploadSession / VM Staging / Chunk Resume
 ↓
P5  File Finalize / NFS Commit / Dedup / GC / Reconciliation
 ↓
P6  Search
 ↓
P7  Background Jobs / Thumbnail / SSE
 ↓
P8  Vue Application Core
 ↓
P9  Upload UI / Preview / PWA
 ↓
P10 Admin / Audit / Runtime Settings
 ↓
P11 Security Hardening / E2E / Performance / Fault Test
 ↓
P12 Podman Quadlet / OpenWrt Deployment / Release
```

## 4. Phase 0 — Repository / Toolchain / CI

### T000 — 初始化标准仓库结构

创建：

```text
cmd/relayshelf/
internal/
web/
api/
migrations/
sql/
infra/
scripts/
docs/
```

**验收：** Production Tree 不包含 old backend / ui prototype / random artifacts。

### T001 — Toolchain Pinning

增加单一工具链声明，例如 `mise.toml`：

- Go；
- Node；
- pnpm。

提交：

```text
go.sum
pnpm-lock.yaml
```

**验收：** 新机器可安装一致版本。

### T002 — Root Task Interface

简单 `Makefile`：

```text
make generate
make lint
make test
make build
make e2e
```

Web 自己的命令保留在 `web/package.json`。

### T003 — OpenAPI Skeleton + Generation

创建：

```text
api/openapi.yaml
```

建立：

- Go HTTP Generated Boundary；
- TypeScript Generated Client。

**验收：**

```text
make generate
git diff --exit-code
```

通过。

### T004 — sqlc Foundation

创建：

```text
sql/sqlc.yaml
sql/queries/
```

Generated DB Types 只由 Repository 包装使用。

### T005 — CI Baseline

CI：

- Generate Check；
- Go fmt/vet/lint；
- Go Unit；
- PostgreSQL Integration；
- Frontend Typecheck/Lint/Unit/Build；
- Selected Race Tests；
- Container Build Smoke。

### Phase 0 Exit Gate

Fresh checkout 可以生成、编译、执行测试，并启动临时 PostgreSQL Test Environment。

## 5. Phase 1 — Config / Schema / Migration

### T010 — Typed Deployment Config

实现：

- DATABASE；
- Storage / Staging Root；
- Encryption / CSRF Secret；
- Public Origin；
- Trusted Proxy；
- Concurrency；
- Staging Quota / Free-space Guard。

Secret 必须 Log Redaction。

### T011 — Migration Runner

Pure SQL Embedded Migration。

CLI：

```text
relayshelf migrate
```

`serve` 检查 Schema Compatibility。

### T012 — Initial Schema

创建：

```text
users
devices
sessions
messages
tags
message_tags
file_objects
message_attachments
file_derivatives
upload_sessions
upload_parts
idempotency_keys
background_jobs
system_settings
audit_logs
```

### T013 — Constraints / Indexes

至少：

- Sensitive plaintext/ciphertext；
- Username uniqueness；
- Tag uniqueness；
- `(sha256,size)`；
- Attachment ref indexes；
- Expiry/Purge indexes；
- Idempotency unique key。

### T014 — pg_trgm Migration

启用 `pg_trgm` 并创建初始 GIN / B-tree Index。

### T015 — Repository Integration Test Harness

所有 DB Integration Tests 执行真实 Migration + PostgreSQL。

### Phase 1 Exit Gate

Empty DB 可以从 0 Migration 到 Current；错误数据被 Constraint 拒绝；Repository Test 基础设施可复用。

## 6. Phase 2 — Users / Auth / Session / CSRF

### T020 — User Domain

实现：

- Username Normalize；
- Create / Disable / Delete primitive；
- Admin role；
- Password hash persistence。

### T021 — Argon2id

实现 Encoded Hash 和 Login 后 Parameter Upgrade。

增加 J4125 Calibration/Benchmark Helper。

### T022 — Login / Session Issue

实现：

- 256-bit Random Token；
- SHA-256 Token Hash；
- HttpOnly Cookie；
- 30d Idle；
- 90d Absolute；
- Device Creation/Reuse。

### T023 — Enumeration / Rate Limit

- Generic Credential Error；
- Dummy Argon2 on Missing User；
- IP + Account In-memory Limiter；
- 防 Audit/Log Amplification。

### T024 — Device / Session Management

- List；
- Rename Device；
- Revoke Session；
- Password Change Revoke Others；
- Admin Reset Revoke All；
- Disabled User Enforcement。

### T025 — CSRF / Origin / Proxy Middleware

- Origin / Referer；
- Session-bound CSRF；
- Trusted Proxy CIDR；
- Public Host Validation；
- Production CORS Disabled。

### T026 — Auth Security Tests

覆盖：

- DB 不存 Raw Token；
- Revoke；
- Absolute Expiry；
- SSE 不延长 Session；
- Password Change；
- Disabled User；
- CSRF Failure；
- Untrusted Forwarded Header ignored。

### Phase 2 Exit Gate

Auth 可以作为所有 Private API 的安全基础。

## 7. Phase 3 — Message / Tag / Sensitive

### T030 — Message CRUD Core

实现：

- Create；
- Detail；
- List；
- Edit；
- Body <= 1MiB；
- Version。

File-only Message 的 Attachment Binding 在 Phase 5 完成，但 Service Contract 提前定义。

### T031 — List Read Model

Cursor Pagination +：

```text
bodyPreview <= 16KiB
bodyTruncated
```

### T032 — Tag Domain

- CRUD；
- Normalize；
- Color Validation；
- Assign；
- AND Combination Filter。

### T033 — Lifecycle / Favorite

- Temporary；
- Permanent；
- Temporary → Permanent；
- expires_at Snapshot；
- Favorite Permanent-only。

### T034 — Trash / Restore / Purge

- Trash Action；
- Trash List；
- Restore；
- purge_at Snapshot；
- Permanent Delete Boundary。

### T035 — Sensitive Crypto

AES-256-GCM：

- Fresh Nonce；
- Version；
- AAD；
- Sensitive Toggle；
- Plaintext/Ciphertext Migration；
- 禁止 Secret/Plaintext Log。

### T036 — Sensitive Body API

显式 Reveal/Copy/Edit API，`Cache-Control: no-store`。

### T037 — Direct Send

- Recipient owner；
- no sender copy；
- Temporary；
- no sender tags；
- Favorite false；
- Source User；
- Sensitive re-encrypt。

### T038 — Forward

- Independent Receiver Message；
- Source Message；
- Attachment reuse hook；
- no tags / favorite；
- Sensitive re-encrypt。

### T039 — Idempotency

Create / Direct Send / Forward：

- Idempotency-Key；
- Request Hash；
- Replay result；
- Different payload reject。

### Phase 3 Exit Gate

Text-only Product Semantics、Sensitive Encryption、User Boundary 全部通过测试。

## 8. Phase 4 — Resumable Upload / VM Staging

### T040 — UploadSession API

Create / Status：

- Server chunk size 8MiB；
- File size limit；
- Upload TTL Snapshot；
- User Ownership。

### T041 — Staging Manager

- Deterministic staging path；
- Sparse/logical file；
- Active Reservation；
- 32GiB Quota；
- VM 10GiB / 10% Free Guard。

### T042 — Chunk PUT

Raw Body Streaming `WriteAt`，不 `io.ReadAll`。

### T043 — UploadPart Persistence

Exact Size 后记录 Completed Part。

Same Part Retry 可以覆盖。

### T044 — Resume Status

返回：

- Immutable Upload Metadata；
- Completed Part Numbers。

### T045 — Complete Validation / Lock

- `SELECT ... FOR UPDATE`；
- State Transition；
- Full Parts / Sizes / Total Validation。

### T046 — Upload Expiration

- Expire Session；
- Delete Staging；
- Clear Parts；
- Completed Session 适度短期保留。

### T047 — Upload Crash Tests

模拟：

- Chunk upload 过程中；
- Part DB Commit 前；
- Complete Transition 过程中。

### Phase 4 Exit Gate

大文件可以分片上传，浏览器重开后重新选择文件恢复，且 Staging 不会把 VM 填满。

## 9. Phase 5 — FileObject / NFS / Dedup / GC

### T050 — FilesystemStorageAdapter Contract

Contract Test：

- Write；
- Read；
- Range；
- Stat；
- fsync；
- Commit/Rename；
- Delete；
- Failure Behavior。

### T051 — `storage check`

CLI：

```text
relayshelf storage check
```

部署时对真实 NFS 检查能力。

### T052 — SHA-256 Finalize

Sequential Hash local staging。

Global Semaphore 默认 1。

### T053 — READY Dedup Lookup

按 `(sha256,size)` 查 READY。

已有则不再写 NAS。

### T054 — PENDING + NAS Commit

- Insert PENDING；
- Stream → `.commit-tmp`；
- fsync；
- Atomic Rename；
- READY；
- UploadSession COMPLETED；
- Delete Staging。

### T055 — Same-file Dedup Test

至少测试两个用户完成相同内容。

最终：

```text
1 FileObject
1 Physical Object
2 Independent Attachment Refs
```

### T056 — MessageAttachment Binding

单 PostgreSQL Transaction：

- Message；
- Attachments；
- Tags；
- Upload Consumed；
- Required Background Jobs；
- Idempotency Result。

只绑定 READY FileObject。

### T057 — Upload Single Consumption

写：

```text
consumed_at
consumed_message_id
```

正常第二次消费必须失败；Same Idempotent Replay 返回第一次结果。

### T058 — Download / HTTP Range

Attachment Owner Authorization + Range + ETag + Safe Content-Disposition + Context Cancellation。

### T059 — FileObject GC

- `NOT EXISTS Attachment refs`；
- 24h orphan grace；
- READY → DELETING；
- Physical Delete；
- Delete Row。

### T060 — Derivative Delete Ordering

Source 完成 Delete 前清理 Derivatives。

### T061 — Reconciliation

处理：

- PENDING + final exists；
- PENDING + no file；
- DELETING + exists；
- DELETING + already absent。

### T062 — Failure Injection Suite

故障点：

- PENDING Insert 后；
- NFS Write；
- fsync；
- rename；
- READY DB Update；
- Physical Delete。

验证：不误删其他 User Shared FileObject，最终可恢复。

### Phase 5 Exit Gate

File Lifecycle Crash Safety 达到 Production 要求。

## 10. Phase 6 — Search

### T070 — Search Query Model

Current User Only：

- body_plaintext；
- original_filename；
- tags / metadata。

Exclude Trash / Sensitive Body。

### T071 — Multi-token AND

Whitespace Tokens + AND。

Minimum Body Query 2 Unicode chars。

### T072 — Filters

- Lifecycle；
- Tags；
- Favorite；
- Type；
- Time Range。

### T073 — Cursor Pagination

Newest-first + opaque cursor。

### T074 — EXPLAIN Plan Test

Synthetic target-scale data，确认正常 Query 使用预期 Index。

### T075 — Search Benchmark

真实环境验证：

```text
~100k Messages
3+ char common query
P95 target < 500ms
```

### Phase 6 Exit Gate

目标规模下无需 External Search Service。

## 11. Phase 7 — Jobs / Thumbnail / SSE

### T080 — BackgroundJob Worker

- State Machine；
- Retry；
- Backoff；
- Stuck Recovery；
- Safe Errors；
- Wake Channel；
- Safety Poll。

### T081 — Atomic Job Creation

Business State + Required Job 同 Transaction。

### T082 — Thumbnail Worker

- Worker count = 1；
- Dimension / Pixel Limit；
- Failure 不影响 Original。

### T083 — FileDerivative

`UNIQUE(source_file_id,kind)` + Authorized Access。

### T084 — SSE Hub

Race-safe user subscriber registry。

### T085 — SSE Endpoint

- Minimal Events；
- originDeviceId；
- Heartbeat；
- Session Expiry Close。

### T086 — Commit Before Publish

所有 Mutation Path 只能 DB Commit 后发 Event。

### T087 — Scheduler

- Hourly；
- Advisory Lock；
- Bounded Batch。

### T088 — Race Detector

重点：

- SSE Hub；
- Worker；
- Rate Limiter；
- Semaphores。

### Phase 7 Exit Gate

跨设备更新和 Thumbnail 跨重启可靠，不引入 Redis/MQ。

## 12. Phase 8 — Vue Core

### T090 — Vue Foundation

Vue3 + TS Strict + Vite + Router + Pinia + TanStack Query。

### T091 — Feature Layout

建立：

```text
auth
messages
uploads
tags
search
trash
sessions
admin
```

### T092 — Generated API Client

普通 API 统一 Generated Client。

### T093 — Auth Shell

Login、Current User Bootstrap、Session Expiry、Device Context。

### T094 — Routes

```text
/temporary
/permanent
/favorites
/tags/:id
/trash
/messages/:id
/admin/*
```

### T095 — MessageFeed / MessageCard

统一核心组件，不为 Temporary/Permanent/Search 各写一套列表。

### T096 — Detail Routing

Desktop Route-driven Overlay / Drawer；Mobile Full Page。

### T097 — Composer Text Flow

- Text / Markdown / Code；
- Sensitive；
- Tags；
- Lifecycle；
- Ctrl+Enter；
- Idempotency Key。

### T098 — Infinite Scroll

IntersectionObserver + Cursor，30 Items/page。

V1 不引 Virtual List。

### T099 — SSE / Query Integration

单 EventSource + Query Invalidate + Reconnect/Visibility Refresh。

### Phase 8 Exit Gate

Text-first 跨设备体验完成。

## 13. Phase 9 — Upload UI / Viewer / PWA

### T100 — Global UploadManager

- Typed State；
- Queue；
- Concurrency；
- Retry；
- Pause/Resume；
- Complete。

### T101 — Composer Upload Integration

Composer 不持有 Transport Logic，只观察 UploadManager。

Send 前要求所有附件 Upload 已 COMPLETED。

### T102 — Resume UX

显示未完成 Upload，提示重新选择原文件。

校验：

- size；
- filename；
- lastModified；
- optional lightweight fingerprint。

### T103 — Attachment Card

List 只给 Attachment Count + 前 3 个摘要。

### T104 — Image Viewer

Lazy Thumbnail；只有打开 Viewer 才请求 Original。

### T105 — Preview Set

- Text/Code；
- PDF；
- Audio/Video；
- Viewer lazy import。

### T106 — Unsafe Attachment

HTML/SVG/XML 不允许 same-origin active render。

### T107 — Markdown / Syntax Security

Raw HTML Off + Sanitization + Safe Link + Dynamic Syntax fallback。

### T108 — PWA App Shell

只缓存 Hash Static Assets。

### T109 — PWA Update UX

新版本提示，不自动 Reload Active Upload。

### Phase 9 Exit Gate

主 File + Message UX 完整。

## 14. Phase 10 — Admin / Audit / Settings

### T110 — Runtime Settings

Typed Singleton API。

### T111 — Storage Status

显示：

- Logical Usage；
- NAS Real Free；
- VM Staging Usage；
- Threshold；
- Degraded State。

### T112 — User Admin

Create / Disable / Reset Password / Delete User，不提供 Browse Private Content 能力。

### T113 — Delete-user Invariant

重点测试 Shared FileObject 在删除一个 User 后仍安全保留。

### T114 — Audit

Event Schema / Metadata Allowlist / Retention Cleanup。

### T115 — Admin Status

- Build Version；
- Migration；
- Health；
- Failed Jobs；
- Storage。

### Phase 10 Exit Gate

管理员具备运维能力，但仍无法浏览其他用户私人内容。

## 15. Phase 11 — Security / E2E / Performance / Fault

### T120 — Security Headers / CSP

启用 CSP、nosniff、Referrer-Policy、Permissions-Policy。

### T121 — Logging Review

证明不会记录：

- Body；
- Sensitive Plaintext；
- Password；
- Session Token；
- Encryption Key；
- Storage Internal Secret。

### T122 — Playwright E2E

关键 Journey：

- Login；
- Send Text；
- 第二浏览器收到 SSE；
- Temp → Permanent；
- Upload；
- Resume；
- Search；
- Direct Send / Forward；
- Trash Restore；
- Sensitive Reveal / Copy。

### T123 — 2 GiB Release Test

真实环境：

- Upload；
- Interrupt；
- Resume；
- SHA-256；
- NFS Commit；
- Range Download。

普通 CI 不跑真实 2GB。

### T124 — NFS Outage

模拟 NAS/NFS 断开：

- Storage degraded；
- 新 Storage Request bounded/rejected；
- 非 Storage Text Path 尽量继续；
- Staging Upload 可以继续；
- 恢复后 Complete Retry 成功。

### T125 — Crash Window

Kill/Restart 等价测试：

- SHA；
- NAS Commit；
- READY DB Update；
- Job RUNNING；
- Delete。

### T126 — Hardware Baseline Script

真实环境测：

- VM Local Sequential R/W；
- NFS Sequential R/W；
- NFS fsync / rename；
- 2GiB SHA-256；
- Thumbnail；
- PostgreSQL Search。

### T127 — Performance Review

根据真实数据调整：

```text
FILE_FINALIZE_CONCURRENCY=1
THUMBNAIL_WORKERS=1
MAX_ACTIVE_CHUNK_WRITES=8
pgx MaxConns=10
chunk=8MiB
```

不为了 2.5GbE Peak 改坏稳定性。

### T128 — Public Exposure Safety Gate

公网前必须确认：

- HTTPS；
- Secure Cookie；
- CSRF；
- Login Rate Limit；
- Trusted Proxy；
- Host/Origin；
- Encryption Key Backup；
- Admin TOTP 已实施并启用。

### Phase 11 Exit Gate

Security / Failure / Performance 在 Reference Hardware 通过。

## 16. Phase 12 — Podman / OpenWrt / Release

### T130 — Production Image

Multi-stage：

1. Node/pnpm Vue Build；
2. Go Generate/Build；
3. Debian-slim 级 Runtime。

Runtime 不含 Node、Go Compiler、源码。

App Container 内 non-root。

### T131 — Quadlet Network / PostgreSQL

- `relayshelf.network`；
- PostgreSQL local VM storage；
- 5432 不暴露 LAN。

### T132 — NFS Host Mount Dependency

Cold Start 确保真实 `/mnt/relayshelf` 已挂载。

### T133 — App Quadlet

Bind：

```text
VM local staging → /staging
NAS NFS mount     → /storage
```

只暴露 App HTTP Port 给 LAN。

### T134 — OpenWrt Nginx Reference

包含：

- HTTPS；
- Forwarded Headers；
- API cache off；
- SSE buffering off；
- Download buffering off；
- Upload request buffering off；
- 16–32MiB Body Limit。

### T135 — Deploy Bundle

Release：

- Quadlet；
- env example；
- install / upgrade / storage-check；
- Nginx Example；
- 简洁 README。

### T136 — Upgrade Preflight

检查：

- NFS Mounted；
- DB Reachable；
- Config Complete；
- Migration Version；
- Free Disk；
- Image Available；
- Backup Warning。

再执行：

```text
migrate
→ restart
→ readiness check
```

### T137 — Version Metadata

- SemVer；
- Git Commit；
- Build Time；
- Exact Image Tag；
- Never `latest`。

### Phase 12 Exit Gate

Clean Debian VM 可以从 Deploy Bundle 安装并在 OpenWrt Nginx 后达到 Healthy Ready 状态。

## 17. Test Strategy Matrix

| Area | Unit | PostgreSQL Integration | Failure Injection | E2E | Real Hardware |
|---|---:|---:|---:|---:|---:|
| Auth/Session | ✓ | ✓ | Limited | ✓ | Optional |
| Messages/Tags | ✓ | ✓ | Limited | ✓ | No |
| Sensitive Crypto | ✓ | ✓ | ✓ | ✓ | No |
| Upload Parts | ✓ | ✓ | ✓ | ✓ | ✓ |
| Finalize/Dedup | ✓ | ✓ | ✓ | ✓ | ✓ |
| FileObject GC | ✓ | ✓ | ✓ | Limited | ✓ |
| Search | ✓ | ✓ | No | ✓ | ✓ |
| Jobs/SSE | ✓ | ✓ | ✓ | ✓ | Optional |
| NFS/Storage | Contract | Metadata | ✓ | Limited | ✓ |
| Deployment | No | No | Smoke | Smoke | ✓ |

## 18. Target-scale Dataset

性能/SQL 设计至少验证：

```text
Users          20
Messages       100,000
Attachments    100,000
Tags           5,000
FileObjects    50,000
AuditLogs      1,000,000
```

这不是 Product Quota，而是用来暴露 O(n) 查询和错误 Index。

## 19. V1 Definition of Done

V1 Ready 必须满足：

1. P0–P12 Exit Gate 全部通过。
2. OpenAPI Generated Code 与 Contract 同步。
3. Fresh DB Migration 与支持版本 Upgrade Migration 正常。
4. `DATA_MODEL.md` Hard Invariants 有自动化测试。
5. 浏览器重开+重新选择文件可 Resume Upload。
6. 相同文件不会正常情况下重复物理存储。
7. 删除最后引用逻辑不会误删仍被他人 Message 引用的 FileObject。
8. Sensitive Body 不会进入 Plaintext Search Storage/Index。
9. NFS 故障与 Crash Window 不造成静默数据损坏。
10. Public Exposure 在 Admin TOTP 等 Safety Gate 前保持关闭。
11. J4125 Real-world Benchmark 证明不会严重抢占 OpenWrt 路由资源或让 VM 不稳定。
12. Production 使用 Exact-version Podman Quadlet，并通过 `/health/ready`。

## 20. Codex / Claude 执行规则

后续给 Coding Agent 的每个 Task 必须明确：

- Task ID；
- Allowed Files / Modules；
- Dependency；
- Required Tests；
- Forbidden Scope Expansion；
- Acceptance Criteria；
- 是否允许修改 Schema；
- 是否允许修改 OpenAPI。

Coding Agent 禁止：

- 顺手大规模重构不相关 Module；
- 私自加入 Redis/MQ/Search Service；
- 私自改变 File Lifecycle；
- 遇到架构冲突时静默“自行设计”。

如果实现任务暴露新的 Design Gap，应停止在明确的 Design Gap 状态并记录问题，再由 Architecture Authority 决定，而不是让 Agent 猜测。
