# RelayShelf — 产品需求文档（PRD）

**状态：** V1 实施基线已冻结  
**版本：** 1.0-draft-authority  
**日期：** 2026-08-25  
**适用对象：** 产品负责人、开发人员、代码审查人员、部署与运维人员

## 1. 产品定位

RelayShelf 是一个面向个人/家庭小规模可信用户的、自托管的、跨设备 **“文件 + 消息剪贴板 / 内容存储”** 系统，用于在 Windows、Linux、Android、iPhone、iPad、Android 平板等设备之间快速投递和取回：

- 普通文本；
- Markdown；
- Shell 命令；
- 代码片段；
- URL；
- JSON / YAML；
- 图片；
- PDF；
- 音视频；
- Office 文件；
- 其他任意附件。

系统的核心交互不是“文件夹”，而是按照时间排序的 **内容卡片流**。

本产品明确：

> 不是聊天软件、不是网盘目录系统、不是文件同步盘、不是团队协作文档系统。

## 2. 产品设计原则

1. **快速投递与快速取回。** 文本和文件都应尽量少操作完成发送。
2. **用户内容默认私有。** 每个用户拥有独立空间；管理员身份不等于可以浏览其他用户私人内容。
3. **轻量自托管。** 面向低功耗家庭硬件，拒绝为了“架构完整”引入无实际收益的基础设施。
4. **文件生命周期可靠。** 分片上传、断点续传、去重、崩溃恢复、回收站、物理文件引用保护是核心能力。
5. **从第一天建立公网安全边界。** 即使 V1 初期只在可信内网运行，也按后续公网暴露需要设计 Session、CSRF、Trusted Proxy、附件安全和加密。
6. **Authority 明确。** PostgreSQL 是业务元数据 Authority；FilesystemStorage 是文件字节 Authority；V1 不引入 Redis、MQ 或独立搜索服务。

## 3. 参考生产环境

正式参考拓扑：

```text
物理机 A：Intel J4125 + 8 GB RAM
├── OpenWrt：2 GB RAM
│   └── Nginx / TLS / 可信反向代理
└── Debian 13 KVM VM：6 GB RAM，约 225 GB 本地磁盘
    ├── Podman Quadlet
    ├── Go 应用
    ├── PostgreSQL
    └── 本地 Upload Staging

2.5 GbE LAN

物理机 B：飞牛 NAS / FNOS
├── 现有 Emby / MoviePilot / 相册 / BT 等应用
└── RelayShelf NFSv4 存储
    ├── objects/
    ├── derivatives/
    └── .commit-tmp/
```

V1 只支持单个 App 实例。

## 4. 用户与角色

### 4.1 用户模型

- 无公开注册。
- 用户由管理员创建。
- 每个用户拥有自己的私人内容空间。
- 管理员是“普通 User + Admin 权限”，不需要第二个管理员专用账号。
- 管理员可以管理：用户、系统设置、存储状态、运行状态、安全审计。
- 管理员**不能**浏览、搜索、下载、编辑、打标签、收藏或选择性删除其他用户的私人消息和附件。
- 管理员可以禁用用户或永久删除整个用户账号。

### 4.2 用户状态

V1：

- `ACTIVE`
- `DISABLED`

用户被禁用后：

- 不能继续登录；
- 既有 Session 不再可用。

## 5. 核心内容模型

### 5.1 Message

Message 是用户看到的主要内容对象，可包含：

- 可选正文；
- 0 个或多个附件；
- 0 个或多个私人标签；
- 生命周期、来源、设备、时间等元数据。

创建 Message 必须至少满足一个条件：

- 正文非空；或
- 至少一个附件。

Message 不要求标题。

### 5.2 临时与长期

Message 生命周期：

- `TEMPORARY`
- `PERMANENT`

规则：

- 临时内容默认保存 **3 天**，可由管理员配置。
- 长期内容永久保存，直到用户删除。
- 临时可转换为长期。
- 长期不能转换回临时。
- 只有长期 Message 才允许收藏。
- 临时 Message 不要求标签。
- 长期 Message 没有标签时仅提醒，不阻止发送。

### 5.3 回收站

普通删除不是物理删除，而是进入 Trash。

- 默认保留 **7 天**，可配置。
- Trash 不进入正常消息流和正常搜索。
- 真正永久删除发生在：手工永久删除、清空 Trash、Trash TTL 到期、删除整个用户等情况。
- 恢复已经过期的临时 Message 时，重新按**当前临时 TTL**计算一个新的 `expires_at`。
- Message 进入 Trash 时写入固定 `purge_at`。
- 管理员后来修改 Trash TTL，不会突然提前删除已经在回收站中的旧 Message。

### 5.4 收藏

- 只有长期 Message 可收藏。
- 提供独立 Favorites 页面。

## 6. 正文需求

### 6.1 文本能力

前端支持：

- Plain Text；
- Markdown；
- URL 识别；
- JSON；
- YAML；
- Shell 命令；
- 代码语法高亮；
- 自动语言识别；
- 用户手动修正语言；
- 一键复制。

常见语言优先内置：

```text
shell
json
yaml
javascript
typescript
python
sql
markdown
```

其他语言可以动态加载或退化为 Plain Text。

### 6.2 Markdown 安全

- 禁止 Raw HTML。
- 输出必须 Sanitization。
- URL protocol 必须 allowlist。
- 外部链接使用安全 `rel`。

### 6.3 正文大小

V1 单条 Message 正文最大：

> **1 MiB**

更大的日志、文本转为 `.txt/.log` 附件上传。

### 6.4 列表正文预览

列表 API / 搜索 API 不能因为允许 1 MiB 正文就一次返回大量正文。

普通非敏感 Message：

```text
bodyPreview   <= 16 KiB
bodyTruncated = true / false
```

- 正文 <= 16 KiB 时，预览就是完整正文。
- 正文 > 16 KiB 时，查看/复制完整内容再请求详情。
- 敏感正文在列表中始终不返回。

## 7. 敏感正文

用户可以将整个正文标记为 Sensitive。

### 7.1 产品行为

- 敏感正文由后端加密。
- 敏感正文不参加全文搜索。
- UI 默认显示锁定状态。
- 用户可以显式显示、复制、编辑。
- 附件在 V1 不加密。
- 普通正文可以切换为敏感；敏感正文也可以切换回普通。
- 切换时必须同步迁移数据库明文/密文状态及搜索可见性。
- Direct Send / Forward 继承 Sensitive 状态。

### 7.2 敏感正文读取

普通 Message API 不直接返回敏感明文。

敏感正文通过独立 owner-authorized 接口读取，例如：

```http
GET /api/v1/messages/{id}/sensitive-body
```

响应必须：

```http
Cache-Control: no-store
```

PWA Service Worker 绝不能缓存该响应。

### 7.3 加密方案

V1：

- AES-256-GCM；
- 部署级 `APP_ENCRYPTION_KEY`；
- 每次加密生成独立随机 nonce；
- 存储 `encryption_version`；
- AAD：`encryptionVersion + messageId + ownerId`。

Direct Send / Forward 敏感正文必须：

```text
解密源 Message
→ 创建新的 Message ID / Owner
→ 生成新 Nonce
→ 使用新 AAD 重新加密
```

禁止直接复制旧 ciphertext。

V1 不支持：

- 密钥轮换；
- Envelope Encryption；
- KMS。

## 8. 标签

- 标签严格 user-private。
- 可创建、重命名、修改颜色、删除。
- 一条 Message 可有多个标签。
- Composer 可选择已有标签或创建新标签。
- 支持多标签组合筛选。
- V1 不做 Tag Merge。
- 同一用户归一化后的标签名唯一。
- Direct Send / Forward **不传递发送方标签**。

## 9. 搜索

### 9.1 搜索范围

搜索当前用户自己的非 Trash 内容：

- 普通 `body_plaintext`；
- Attachment 原始文件名；
- 标签；
- 允许的元数据。

不搜索：

- 敏感正文；
- 附件内部内容；
- Trash。

### 9.2 搜索实现

V1 只使用 PostgreSQL：

- `pg_trgm`；
- GIN / B-tree；
- SQL joins。

不引入：

- Elasticsearch；
- Meilisearch；
- Typesense；
- jieba/zhparser 服务。

### 9.3 搜索语义

- 正文搜索至少 2 个 Unicode 字符。
- 多关键词以空白拆分，采用 **AND** 语义。
- V1 不做 Gmail 式高级搜索语法。
- 生命周期、标签、收藏、类型、时间通过 UI Filter 提供。
- 时间筛选：全部、24 小时、7 天、30 天、自定义。
- V1 结果默认以新内容优先，而不是复杂相关度排序。
- 普通 3+ 字符搜索，在目标规模下目标 P95 < 500ms。
- 很短的 2 字符搜索保留功能，但性能 Best Effort。

## 10. Direct Send 与 Forward

系统不是聊天软件，因此没有：

- 会话；
- 回复；
- 已读/未读；
- 未读数；
- 群组。

### 10.1 Direct Send

用户可直接创建一条属于另一用户的新 Message。

规则：

- Sender 不保留 Message 副本。
- Receiver 得到独立 Message。
- Receiver 为 owner。
- 生命周期强制 `TEMPORARY`。
- 初始 tags = 空。
- `favorite=false`。
- 保存 `source_user_id`。
- 不要求 `source_message_id`。
- Sensitive 正文重新加密。

如果请求同时包含：

- Sender 的 tag IDs；
- Permanent lifecycle；

则返回 422，而不是静默忽略。

### 10.2 Forward

Forward 已有 Message：

- Sender 保留原 Message。
- Receiver 得到独立 Message。
- Receiver Message 默认 `TEMPORARY`。
- Receiver tags 为空。
- `favorite=false`。
- 保存 source user 与 source message。
- 附件可以复用同一个物理 FileObject。
- Sensitive 正文重新加密。

Forward 后两边 Message 独立编辑、删除、生命周期转换、标签等。

## 11. 附件与文件

### 11.1 文件限制

- 默认单文件最大 **2 GiB**，管理员可配置。
- 单条 Message 的附件总大小 V1 不额外设上限。
- 有系统级逻辑存储容量限制。
- V1 没有 per-user quota。

### 11.2 文件输入方式

支持：

- 文件选择；
- Drag & Drop；
- Ctrl+V 粘贴图片；
- 手机选择文件/照片。

### 11.3 分片上传与断点续传

V1 必须支持服务端断点续传。

默认：

```text
chunk size                  8 MiB
per-file browser concurrency 2
global browser concurrency   4
server active chunk writes   8
incomplete upload retention  24h
```

浏览器关闭后不做后台上传。重新打开后，用户重新选择原文件，再根据已完成 Part 继续。

客户端不计算完整文件 SHA-256，不做客户端秒传。

### 11.4 VM 本地 Staging

分片写 Debian VM 本地磁盘，不直接随机写 NFS。

参考部署：

```text
staging quota        32 GiB
VM free guard        < 10 GiB 或 < 10%
```

达到任一安全线后拒绝创建新的文件 UploadSession；纯文本仍可正常使用。

### 11.5 Complete 与 SHA-256 去重

Complete：

1. 检查全部 Part 是否齐全；
2. 检查每个 Part 大小；
3. 检查总大小；
4. 顺序读取 VM staging；
5. 服务端计算 SHA-256；
6. 按 `(sha256,size)` 检查 Global Dedup；
7. 已存在 READY FileObject → 直接复用；
8. 不存在 → 新建 PENDING FileObject；
9. 顺序写 NAS `.commit-tmp`；
10. `fsync`；
11. 原子 rename 到 `objects/`；
12. FileObject → READY；
13. UploadSession → COMPLETED；
14. 删除 VM staging。

参考 J4125 VM：

> `FILE_FINALIZE_CONCURRENCY=1`

### 11.6 Global Dedup 隐私取舍

全用户共享物理去重：

```text
UNIQUE(sha256,size)
```

普通用户 API 不暴露：

- FileObject ID；
- storage key；
- 全局 hash。

V1 接受一个弱时间侧信道：登录用户如果上传一个自己已经猜到内容的完全相同文件，可能从 Complete 时间差推测系统里已有该文件。

不为了掩盖该侧信道而故意重复写 NAS。

### 11.7 Attachment 与 FileObject

- 原始文件名属于 MessageAttachment。
- 同一个 FileObject 可以被多个 MessageAttachment 引用，并且每个 Attachment 可以有不同文件名。
- 物理路径只使用内部 ID，不使用用户文件名。

### 11.8 文件预览

V1：

- 图片：Thumbnail + Viewer + Zoom + 上下张 + 下载；
- 文本/代码：预览前约 1 MiB；
- PDF：浏览器/PDF Viewer；
- 音视频：HTML5 原生播放；
- 不做视频转码；
- Office：仅下载。

HTML / SVG / XML 等 Active Content 不能在主站 Origin 直接执行。

### 11.9 Range Download

单文件必须支持 HTTP Range 和断点下载。

下载路径以 Attachment 为授权对象，例如：

```http
GET /api/v1/attachments/{attachmentId}/download
```

不能直接暴露 FileObject 下载接口。

### 11.10 ZIP

多附件 ZIP 延迟到 V1.1。

未来实现时：

- Low compression / store；
- 不承诺断点续传。

## 12. 物理文件生命周期

Authority：

> `message_attachments` 是否存在引用。

规则：

- Trash Message 的 Attachment 仍算引用。
- 最后一个 Attachment 引用消失之前禁止删除 FileObject。
- V1 不维护 authoritative `reference_count` 字段。
- READY 且 0 引用的 FileObject 先保留约 24 小时 orphan grace。
- 删除状态：

```text
READY → DELETING → Storage.Delete → 删除 DB row
```

Storage 删除失败则保持 `DELETING`，后续重试。

FileObject 删除前先删除 Derivatives。

## 13. Derivative

使用通用 `file_derivatives` 模型。

V1 第一种 derivative：图片缩略图。

- Derivative 属于 FileObject，而不是 Attachment。
- `(source_file_id,kind)` 唯一。
- `THUMBNAIL_WORKERS=1`。
- Thumbnail 失败不影响原文件 READY 状态。
- 必须有最大宽高/最大像素等解压炸弹防护。

## 14. 登录与 Session

### 14.1 密码

- 用户名 + 密码。
- 密码最少 10 字符。
- 不强制大小写/符号组合规则。
- Argon2id。
- 参数存进 encoded hash。
- 登录成功后允许自动升级旧参数。
- 参数在真实 J4125 上标定，目标约 100–250ms/次验证，而不是照抄服务器参数。

### 14.2 Session

V1 使用服务端 Session，不用 JWT 做登录会话。

```text
Browser raw token
→ SHA-256
→ sessions.token_hash
```

Token：

- 256-bit CSPRNG；
- 不包含 user ID；
- 不包含 role；
- 不包含时间。

Session：

```text
idle timeout       30 days
absolute lifetime  90 days
```

支持：

- 多设备；
- 自定义设备名；
- Session 管理；
- 单 Session revoke；
- 修改密码撤销其他 Session；
- Admin 重置密码撤销目标用户全部 Session。

SSE heartbeat 不算“用户活动”，不能让 Session 永久续命。

### 14.3 Cookie / CSRF

- HttpOnly Cookie。
- 公网 HTTPS 使用 Secure Cookie。
- SameSite。
- 写操作检查 Origin/Referer。
- 写操作还需要 CSRF Token。
- CSRF Token 可通过 `CSRF_SECRET + Session` HMAC 派生。
- 生产环境默认不开放通用 CORS。

### 14.4 登录防护

- 用户不存在和密码错误返回相同 `AUTH_INVALID_CREDENTIALS`。
- 用户不存在时仍执行 dummy Argon2 Verify，减小 timing enumeration。
- 进程内按 IP + normalized username 限流。
- 不做永久锁号。
- 高频攻击不能导致 Audit/Log 本身成为 DB DoS。

## 15. TOTP

TOTP 延迟到准备公网暴露时实施。

要求：

- Admin 必须启用 TOTP；
- 普通用户可选；
- TOTP Secret 独立表/独立加密结构；
- 不把半成品 TOTP 提前塞入 V1。

公网 Safety Gate 必须包含管理员 TOTP。

## 16. SSE 实时更新

V1 使用 Server-Sent Events。

- 一个浏览器 App 只建立一个全局 EventSource。
- 同一用户可有多个设备/连接。
- SSE 只传资源变化元数据，不传正文、附件列表、密文。
- 事件可包含：event ID、type、resource ID、version、originDeviceId、time。
- 同账号所有设备都可收到事件。
- 当前设备可根据 `originDeviceId` 去重。
- Direct Send / Forward 的新 Message 事件发送给 Receiver。
- SSE Hub 只在内存里。
- V1 不做 durable event history / replay。
- 断线重连后重新 invalidate/refetch 当前视图。
- Heartbeat 约 25 秒。
- 必须先 DB Commit，再 Publish SSE。
- V1 不为了 UI Hint 引入 Transactional Outbox。

## 17. PWA

V1 Online-first。

Service Worker 只缓存：

- JS；
- CSS；
- icons；
- manifest；
- 静态 App Shell。

不缓存：

- `/api/v1/*`；
- Message 私人正文；
- sensitive-body；
- Attachments；
- Search Results。

不做：

- 完整离线历史；
- 离线上传队列；
- Background Sync 大文件上传。

未发送的正文草稿不持久化到 LocalStorage/IndexedDB。

发现新 PWA 版本时提示用户刷新，不能自动打断正在进行的大文件上传。

## 18. Admin 与 Runtime Settings

Admin 页面：

- Users；
- Storage；
- Settings；
- System Status；
- Audit。

Runtime Settings 存 PostgreSQL：

- Temporary TTL；
- Trash TTL；
- Max File Size；
- Max Logical Storage；
- Audit Retention；
- Upload Retention。

Deployment Config：

- DATABASE；
- Storage Root；
- Staging Root；
- Encryption Key；
- CSRF Secret；
- Public Origin；
- Trusted Proxies；
- 并发/性能参数。

Deployment Config 进程启动后不可动态修改。

## 19. 容量管理

### 19.1 NAS 逻辑存储阈值

按去重后的物理 FileObject 字节统计：

- 80%：Admin warning；
- 90%：Strong warning + User warning；
- 100%：禁止新文件上传。

达到 100% 后仍允许：

- 纯文本；
- 下载；
- 删除；
- 其他非新增文件功能。

### 19.2 NAS 真实 Free Space

逻辑配额不能代替真实磁盘检查。

同时检查绝对剩余空间与百分比剩余空间阈值。

### 19.3 VM Staging

VM Staging 是独立容量体系，不和 NAS Logical Storage 混算。

## 20. Audit 与日志

### 20.1 Security Audit

至少记录：

- 登录成功/失败；
- Logout；
- 密码修改；
- 用户创建/禁用/删除；
- Session revoke；
- Permanent Delete；
- System Settings Change；
- 未来的 TOTP 事件。

默认 Audit Retention：90 天，可配置。

禁止记录：

- Message body；
- File content；
- Password；
- Raw Session Token；
- Encryption Key；
- CSRF Token；
- 其他 Secret。

### 20.2 Application Log

可以记录：

- method；
- route template；
- status；
- duration；
- trace ID；
- authenticated user ID；
- 经过 Trusted Proxy 规则解析后的 IP。

默认禁止 request-body logging。

## 21. Health 与运维

必须提供：

```text
/health/live
/health/ready
```

- `live`：只表示进程存活。
- `ready`：反映 DB、Storage 等依赖状态，可 degraded/not-ready。

Public `live` 不泄露：

- Go Version；
- PostgreSQL Version；
- Git SHA。

详细版本只在 Admin Status 中展示。

## 22. 后台任务

### 22.1 Persistent Background Jobs

用于需要跨重启可靠执行的工作，V1 主要是 Thumbnail。

状态：

```text
PENDING
RUNNING
COMPLETED
FAILED
```

支持：

- attempts；
- next_run_at；
- safe error code；
- 有限指数退避；
- stuck RUNNING 恢复。

业务状态和要求它存在的 Job Row 必须在同一 PostgreSQL Transaction 创建。

Worker：

- Commit 后用内存 channel 唤醒；
- 另有 30–60 秒 Safety Poll；
- 空闲时不能高频轮询 DB。

### 22.2 Maintenance

大约每小时：

- Temporary Expiry；
- Trash Purge；
- Orphan FileObject；
- DELETING Retry；
- Upload Expiry；
- Audit Retention；
- Stuck Job Recovery；
- Derivative Cleanup。

使用 PostgreSQL Advisory Lock，批量 100–500 行短事务处理。

## 23. API 需求

- Base：`/api/v1`。
- OpenAPI 是 HTTP Contract Authority。
- JSON camelCase。
- DB snake_case。
- ID：UUIDv7，对 API 表示为 string。
- 时间：UTC + RFC3339。
- 成功响应直接返回 Resource，不套通用 `{code,data}`。
- Error：`code/message/traceId/details`。
- 前端逻辑只能判断 `code`。
- Cursor Pagination：default 30 / max 100。
- Cursor opaque。
- Message Create / Direct Send / Forward 支持 `Idempotency-Key`。
- Message Update 使用 `expectedVersion`。

## 24. 批量操作

V1 只要求有限批量：

- Delete to Trash；
- Temporary → Permanent；
- Add/Remove Tags；
- Favorite/Unfavorite。

## 25. V1 明确不做

- Chat；
- Reply；
- Group；
- Read/Unread；
- Public Signup；
- Anonymous Upload；
- Public Share Link；
- E2EE；
- Attachment Encryption；
- Office Online Editing；
- Video Transcoding；
- Online File Collaboration；
- Folder Drive；
- Dropbox-style Sync；
- Native Desktop/Mobile Client；
- Full Offline；
- Client-side Instant Upload；
- Version History；
- Per-user Quota；
- Built-in Backup Engine；
- External Search Service；
- Redis / MQ；
- Multi-App Instance；
- URL/OpenGraph 自动抓取；
- Server-side ZIP 自动解压。

## 26. V1 产品验收摘要

V1 完成时，可信小规模用户应能可靠完成：

1. 登录、设备和 Session 管理；
2. 创建临时/长期文本和文件 Message；
3. 大文件分片上传及浏览器关闭后的恢复；
4. 服务端 SHA-256 Global Dedup；
5. 普通正文/文件名/标签搜索；
6. Sensitive 正文加密并排除搜索；
7. 用户之间 Direct Send / Forward；
8. 编辑、标签、收藏、Trash、Restore、Purge；
9. 跨设备 SSE 更新；
10. 安全预览与 Range Download；
11. Process Crash / NFS 典型故障窗口可恢复且不误删数据；
12. 在 J4125 + 6GB VM 环境保持可接受资源占用；
13. 通过 Podman Quadlet + OpenWrt Nginx + NFSv4 NAS 完成生产部署。
