# RelayShelf — 数据模型设计

**状态：** V1 Logical Data Model Authority  
**版本：** 1.0-draft-authority  
**日期：** 2026-08-25

## 1. 全局约定

- Database：PostgreSQL 17。
- 主键：UUIDv7。
- API 中 ID 序列化为 string。
- 时间字段：`TIMESTAMPTZ`，统一 UTC。
- 表名/列名：`snake_case`。
- Message 普通删除通过 `trashed_at` 表示；永久删除才真正 DELETE row。
- 物理文件引用以关系表为 Authority，不维护 authoritative reference count。
- Sensitive Message 通过明文/密文互斥列实现，并由数据库 Constraint + Service 双重保护。

## 2. 关系总览

```text
User
├── Device
│   └── Session
├── Message
│   ├── MessageAttachment ──> FileObject ──> FileDerivative
│   └── MessageTag ─────────> Tag
├── UploadSession
│   └── UploadPart
├── AuditLog
└── IdempotencyKey

SystemSettings（Singleton）
BackgroundJob
```

## 3. `users`

用途：用户 Identity、账号状态、管理员权限。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | UUIDv7 PK |
| `username` | TEXT | Normalized login name |
| `display_name` | TEXT | 可重复 |
| `password_hash` | TEXT | Encoded Argon2id |
| `is_admin` | BOOLEAN | default false |
| `status` | TEXT/enum | `ACTIVE` / `DISABLED` |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

约束：

- Normalized Username 全局唯一。
- Username Case-insensitive 通过 Normalize 处理。
- Display Name 可重复。

V1 不把 TOTP Secret 直接放进 users。

## 4. `devices`

用途：用户可见设备身份、Session Group。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | PK |
| `user_id` | UUID | FK users |
| `name` | TEXT | 用户可修改设备名 |
| `user_agent` | TEXT | Safe metadata |
| `first_seen_at` | TIMESTAMPTZ | |
| `last_seen_at` | TIMESTAMPTZ | 节流更新 |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

Index：

```text
(user_id,last_seen_at DESC)
```

## 5. `sessions`

用途：Opaque Server-side Session。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | Internal Session ID |
| `user_id` | UUID | FK users |
| `device_id` | UUID | FK devices |
| `token_hash` | BYTEA | SHA-256(raw token)，unique |
| `expires_at` | TIMESTAMPTZ | Idle expiry |
| `absolute_expires_at` | TIMESTAMPTZ | 90d hard cap |
| `last_seen_at` | TIMESTAMPTZ | 节流更新 |
| `last_ip` | INET/TEXT | Safe metadata |
| `revoked_at` | TIMESTAMPTZ NULL | revoke marker |
| `created_at` | TIMESTAMPTZ | |

约束 / Index：

- `token_hash` unique；
- `(user_id,revoked_at,expires_at)`；
- Session 有效还要求 `users.status=ACTIVE`。

禁止存 Raw Session Token。

## 6. `messages`

用途：用户私有内容对象。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | UUIDv7 PK |
| `owner_id` | UUID | FK users，Privacy Authority |
| `body_plaintext` | TEXT NULL | 普通正文 |
| `body_ciphertext` | BYTEA NULL | Sensitive 正文 |
| `body_nonce` | BYTEA NULL | Sensitive only |
| `body_encryption_version` | SMALLINT NULL | Sensitive only |
| `body_format` | TEXT | Text/Markdown 等 |
| `detected_type` | TEXT NULL | code/url/json/yaml/... |
| `detected_language` | TEXT NULL | 识别/人工覆盖 |
| `sensitive` | BOOLEAN | |
| `lifecycle` | TEXT/enum | `TEMPORARY` / `PERMANENT` |
| `is_favorite` | BOOLEAN | Service 只允许 Permanent |
| `expires_at` | TIMESTAMPTZ NULL | Temporary deadline snapshot |
| `trashed_at` | TIMESTAMPTZ NULL | Trash marker |
| `purge_at` | TIMESTAMPTZ NULL | Trash purge deadline snapshot |
| `source_user_id` | UUID NULL | Sender/Forward source |
| `source_message_id` | UUID NULL | Forward source，ON DELETE SET NULL |
| `created_device_id` | UUID NULL | Provenance |
| `version` | BIGINT | Optimistic concurrency，从 1 开始 |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

关键 Constraint：

### 6.1 Plaintext / Ciphertext 互斥

```text
sensitive=false
→ body_ciphertext/body_nonce/body_encryption_version IS NULL

sensitive=true
→ body_plaintext IS NULL
→ ciphertext/nonce/version 必须存在（有正文时）
```

文件-only Sensitive 状态是否允许应由 API Contract 明确定义；推荐 Sensitive 只对正文有意义，文件-only Message `sensitive=false`。

### 6.2 Lifecycle

```text
TEMPORARY → active 时 expires_at 通常非 NULL
PERMANENT → expires_at NULL
```

### 6.3 Favorite

```text
is_favorite=true → lifecycle=PERMANENT
```

可 Service + CHECK 双重保护。

### 6.4 Body-or-Attachment

这是 Cross-table Invariant，不能靠单表 CHECK：

> Message Service Transaction 保证最终必须有正文或至少一个 Attachment。

Indexes：

```text
(owner_id,lifecycle,trashed_at,created_at DESC,id DESC)
(owner_id,is_favorite,trashed_at,created_at DESC,id DESC)
(owner_id,purge_at)
(owner_id,expires_at)
```

普通正文建立 Partial Trigram GIN Index。

## 7. `file_objects`

用途：全局去重后的物理文件对象。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | PK / Storage Key ID |
| `sha256` | BYTEA | 32 bytes |
| `size_bytes` | BIGINT | |
| `detected_mime` | TEXT | Server Authority |
| `storage_backend` | TEXT | V1=`filesystem` |
| `storage_key` | TEXT | Internal key，不是文件名 |
| `status` | TEXT/enum | `PENDING`,`READY`,`DELETING` |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |
| `ready_at` | TIMESTAMPTZ NULL | 可选 |

核心：

```text
UNIQUE(sha256,size_bytes)
```

只允许 READY FileObject 绑定 MessageAttachment。

不建立 authoritative `reference_count`。

## 8. `message_attachments`

用途：Message 与 FileObject 的用户可见关系。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | User-facing Attachment ID |
| `message_id` | UUID | FK messages ON DELETE CASCADE |
| `file_object_id` | UUID | FK file_objects |
| `original_filename` | TEXT | 用户看到的原文件名 |
| `client_mime` | TEXT NULL | Advisory only |
| `display_order` | INTEGER | |
| `metadata` | JSONB | 受控安全 metadata，可选 |
| `created_at` | TIMESTAMPTZ | |

Index：

```text
(message_id,display_order)
file_object_id
```

安全预览使用 `file_objects.detected_mime`，不信任 `client_mime`。

## 9. `file_derivatives`

用途：Thumbnail 等派生物理文件。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | PK |
| `source_file_id` | UUID | FK file_objects |
| `kind` | TEXT | 如 `THUMBNAIL_SMALL` |
| `storage_key` | TEXT | derivatives path |
| `mime` | TEXT | Generated MIME |
| `size_bytes` | BIGINT | |
| `status` | TEXT/enum | PENDING/READY/FAILED/DELETING（按实现需要） |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

Constraint：

```text
UNIQUE(source_file_id,kind)
```

Derivative 不能因为知道 ID 就公开访问，必须从 Attachment/Message 权限路径鉴权。

## 10. `tags`

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | PK |
| `user_id` | UUID | FK users |
| `name` | TEXT | Display form |
| `normalized_name` | TEXT | Uniqueness/Search |
| `color` | TEXT | Validated UI color |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

Constraint：

```text
UNIQUE(user_id,normalized_name)
```

## 11. `message_tags`

| Column | Type | 说明 |
|---|---|---|
| `message_id` | UUID | FK messages ON DELETE CASCADE |
| `tag_id` | UUID | FK tags ON DELETE CASCADE |
| `created_at` | TIMESTAMPTZ | 可选 |

Primary Key：

```text
(message_id,tag_id)
```

Service 必须保证：

```text
Message.owner_id == Tag.user_id
```

## 12. `upload_sessions`

用途：断点上传、VM Staging、Complete 状态。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | PK |
| `user_id` | UUID | FK users |
| `original_filename` | TEXT | |
| `expected_size` | BIGINT | |
| `client_mime` | TEXT NULL | |
| `chunk_size` | BIGINT | Server-selected |
| `status` | TEXT/enum | 见下 |
| `file_object_id` | UUID NULL | Complete 后解析 |
| `expires_at` | TIMESTAMPTZ | Upload TTL snapshot |
| `consumed_at` | TIMESTAMPTZ NULL | Single consumption |
| `consumed_message_id` | UUID NULL | 绑定结果 |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |
| `completed_at` | TIMESTAMPTZ NULL | |

Status：

```text
CREATED
UPLOADING
COMPLETING
COMPLETED
FAILED
EXPIRED
```

Index：

```text
(user_id,status,expires_at)
```

数据库不存 Staging Absolute Path，路径由：

```text
STAGING_ROOT + UploadSession ID
```

确定。

## 13. `upload_parts`

用途：已完成 Chunk 的 Durable State。

| Column | Type | 说明 |
|---|---|---|
| `upload_session_id` | UUID | FK upload_sessions ON DELETE CASCADE |
| `part_number` | INTEGER | 明确定义 zero-based |
| `size_bytes` | BIGINT | Exact received size |
| `completed_at` | TIMESTAMPTZ | |

Primary Key：

```text
(upload_session_id,part_number)
```

V1 不存 per-part SHA-256。

## 14. `idempotency_keys`

用途：Message Create / Direct Send / Forward 防止 HTTP Retry 产生重复内容。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | PK |
| `user_id` | UUID | FK users |
| `operation` | TEXT | create/direct-send/forward |
| `key` | TEXT | Client random key |
| `request_hash` | BYTEA | Canonical request hash |
| `resource_type` | TEXT NULL | Safe result metadata |
| `resource_id` | UUID NULL | Created Message ID 等 |
| `response_metadata` | JSONB NULL | 小型安全结果，不放正文/Secret |
| `created_at` | TIMESTAMPTZ | |
| `expires_at` | TIMESTAMPTZ | 约 24h |

Constraint：

```text
UNIQUE(user_id,operation,key)
```

Same Key + Different Hash：

```text
IDEMPOTENCY_KEY_REUSED
```

## 15. `background_jobs`

用途：跨进程重启可靠的异步工作。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | PK |
| `job_type` | TEXT | 如 `GENERATE_THUMBNAIL` |
| `subject_type` | TEXT | Target type |
| `subject_id` | UUID | FileObject 等 |
| `status` | TEXT/enum | PENDING/RUNNING/COMPLETED/FAILED |
| `attempts` | INTEGER | |
| `next_run_at` | TIMESTAMPTZ | Retry schedule |
| `started_at` | TIMESTAMPTZ NULL | |
| `last_error_code` | TEXT NULL | Safe code |
| `last_error_summary` | TEXT NULL | Sanitized bounded message |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

Index：

```text
(status,next_run_at)
```

需要时可以为 `(job_type,subject_id)` 增加 Dedup 约束。

要求 Job 的业务状态与 Job Row 同事务创建。

## 16. `system_settings`

强类型 Singleton，不做 Generic KV。

| Column | Type | 说明 |
|---|---|---|
| `id` | SMALLINT | Singleton，CHECK id=1 |
| `temporary_ttl_hours` | INTEGER | default 72 |
| `trash_ttl_hours` | INTEGER | default 168 |
| `max_file_size_bytes` | BIGINT | default 2 GiB |
| `max_storage_bytes` | BIGINT | Admin configured |
| `audit_retention_days` | INTEGER | default 90 |
| `upload_retention_hours` | INTEGER | default 24 |
| `updated_at` | TIMESTAMPTZ | |
| `updated_by_user_id` | UUID NULL | Admin provenance |

TTL 修改只影响新的 Deadline Snapshot，不自动重写历史 `expires_at/purge_at`。

## 17. `audit_logs`

Append-only Security Audit。

| Column | Type | 说明 |
|---|---|---|
| `id` | UUID | PK |
| `actor_user_id` | UUID NULL | 未认证事件可 NULL |
| `event_type` | TEXT | Stable code |
| `target_type` | TEXT NULL | |
| `target_id` | UUID NULL | |
| `ip` | INET/TEXT NULL | Trusted Proxy 后解析 |
| `user_agent` | TEXT NULL | Bounded |
| `device_id` | UUID NULL | |
| `session_id` | UUID NULL | Internal ID，不是 token |
| `trace_id` | TEXT/UUID NULL | |
| `metadata` | JSONB | Event-specific allowlist |
| `created_at` | TIMESTAMPTZ | |

Index：

```text
(created_at DESC)
(actor_user_id,created_at DESC)
(event_type,created_at DESC)  # 需要时
```

禁止保存：

- Body；
- File Content；
- Password；
- Raw Session Token；
- Encryption Key；
- CSRF Token。

## 18. Future `user_totp`

不属于初始 V1 功能。

未来独立表，例如：

```text
user_totp
├── user_id
├── secret_ciphertext
├── encryption_version
├── enabled_at
└── recovery_code representation
```

禁止把 Plaintext TOTP Secret 放 `users`。

## 19. Search Index

启用：

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

普通 Message Body：

```sql
CREATE INDEX ...
ON messages
USING gin (body_plaintext gin_trgm_ops)
WHERE body_plaintext IS NOT NULL
  AND trashed_at IS NULL;
```

Attachment Filename：

```sql
CREATE INDEX ...
ON message_attachments
USING gin (original_filename gin_trgm_ops);
```

Tag：

```text
(user_id,normalized_name)
```

Search Query 仍必须通过 Message owner join 限制用户权限；Index 本身不是授权机制。

## 20. File GC 查询

是否还有引用，Authority 为：

```sql
NOT EXISTS (
  SELECT 1
  FROM message_attachments ma
  WHERE ma.file_object_id = file_objects.id
)
```

Trash Message 的 Attachment Row 仍存在，因此自然阻止物理删除。

只有：

```text
READY
+ 0 refs
+ orphan grace expired
```

才可进入 `DELETING`。

## 21. Temporary Expiry / Trash Purge

### Expire

```text
TEMPORARY
expires_at <= now()
trashed_at IS NULL
       ↓
trashed_at = now()
purge_at   = now() + current Trash TTL
```

### Restore

- 清空 `trashed_at` / `purge_at`。
- 如果 Temporary 已经过期，重新按当前 Temporary TTL 写新的 `expires_at`。

### Purge

```text
purge_at <= now()
→ DELETE Message
→ MessageAttachment Cascade Delete
→ FileObject 以后由 Orphan GC 判断
```

不要在 Message 删除事务里立即物理 Delete FileObject。

## 22. Direct Send / Forward 数据语义

### 22.1 Direct Send

```text
owner_id          = recipient
source_user_id    = sender
source_message_id = NULL
lifecycle         = TEMPORARY
is_favorite       = false
```

不绑定 Sender Tags。

### 22.2 Forward

```text
owner_id          = recipient
source_user_id    = sender
source_message_id = original message
lifecycle         = TEMPORARY
is_favorite       = false
```

`source_message_id ON DELETE SET NULL`。

Sender 删除原 Message 不影响 Receiver 独立副本。

Attachment 通过新 MessageAttachment 引用原 FileObject。

## 23. 删除整个 User

Admin 可以 Delete User，但不能浏览其私人内容。

必须由专门 User Deletion Service Workflow 实现：

1. Revoke Sessions；
2. Purge User Messages / Relations；
3. 删除 Tags / Devices / Account Metadata；
4. 其他用户仍引用的 FileObject 必须保留；
5. 只有最终 0 refs 的 FileObject 才交给 GC；
6. 写安全 Audit，不记录私人内容。

不能在 Admin Handler 临时拼 DELETE SQL。

## 24. FileObject / Filesystem Reconciliation

PostgreSQL 与 NFS 无跨系统 Transaction。

以下状态都必须被视为可恢复：

```text
PENDING + commit temp exists
PENDING + final object exists
PENDING + no object
DELETING + object still exists
DELETING + object already absent
```

Reconciliation 只使用 internal storage key，禁止依赖用户文件名推断状态。

## 25. Data Model Hard Invariants

Release 必须保持：

1. 每条私人 Message 都有 `owner_id`。
2. Tag Assignment 不能跨 User。
3. `sensitive=true` 时不得保留 `body_plaintext`。
4. `sensitive=false` 时不得残留 Sensitive Ciphertext Metadata。
5. FileObject Global Dedup 唯一键是 `(sha256,size)`。
6. Filename 属于 Attachment Relation。
7. 有任何 Attachment Ref 时禁止删除 FileObject。
8. Trash Ref 也属于有效 File Ref。
9. Completed UploadSession Single Consumption。
10. Stale Message Version 不能覆盖新版本。
11. Direct Send / Forward Receiver Message 独立。
12. Source Message 删除不能破坏 Receiver Message。
13. Idempotency Key 不能用不同 Payload Replay。
14. Required BackgroundJob 必须和业务状态同事务创建。
15. Runtime TTL 修改不能静默重写历史 Deadline。
