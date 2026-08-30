# RelayShelf UI 重构执行计划

> 面向 Codex 256K 上下文窗口的分阶段实施文档。
>
> 本文档的源代码分析基线为 `a4799f1775571a7add35b75b909b2f16882e3440`（2026-08-30，`feat(admin): add initial administrator bootstrap`）。执行时以包含本文档的最新 `main` 为起点；之后每个任务必须从前一个任务完成后的最新提交继续，不得 reset 回本文档记录的分析基线。

## 1. 目标与最终 UI 基线

本次工作是 **UI architecture refactor**，不是业务重写。最终 RelayShelf 应从传统 Feed/CRUD 页面重构为跨设备内容工作台，同时保持现有后端 API、数据模型、上传协议、安全语义和消息生命周期不变。

### Desktop

桌面端采用三栏 Power User 布局：

```text
┌──────────────┬──────────────────────────────────┬──────────────────────────┐
│              │ Topbar                           │ Topbar                   │
│              │ Global Search        Sync/Theme  │                          │
│              ├──────────────────────────────────┼──────────────────────────┤
│ Left Sidebar │                                  │                          │
│              │      Floating Composer           │                          │
│ Navigation   │                                  │     Detail Inspector     │
│              │      Feed Toolbar                │                          │
│              │                                  │     selected message     │
│              │      Message Feed                │     metadata/actions     │
│              │                                  │     attachments          │
│              │                                  │     lifecycle            │
│ Storage      │                                  │                          │
│ User         │                                  │                          │
└──────────────┴──────────────────────────────────┴──────────────────────────┘
```

约束：

- 第一栏：固定主导航。
- 第二栏：核心工作区。
- 第三栏：全部用于内容详情，不放 Composer。
- Composer 位于第二栏顶部，不铺满第二栏；使用 `max-width` 和左右留白形成悬浮卡片。
- Feed 与 Composer 位于同一内容轴。
- 每条 MessageCard 都提供直接复制入口。
- 点击 MessageCard 在第三栏打开详情，不跳离当前上下文。
- 普通完整正文不打开详情即可复制；Sensitive / truncated 内容不能绕过现有安全边界。
- 附件使用更丰富的图片/文件卡片表现，但沿用现有 thumbnail、安全 MIME 和 viewer 逻辑。
- 存储状态和设备/连接状态只显示一处，最终保留在左侧栏底部；Topbar 不重复显示。
- 提供 `system` / `light` / `dark` 三种主题模式。

### Mobile / Tablet

小于桌面三栏阈值时使用移动端方案 A：

```text
┌─────────────────────────┐
│ Mobile Header           │
├─────────────────────────┤
│                         │
│ Floating Composer       │
│                         │
│ Feed Toolbar            │
│                         │
│ Message Feed            │
│                         │
├─────────────────────────┤
│ 临时  长期  搜索  上传  我的 │
└─────────────────────────┘
```

- 不常驻第三栏。
- 点击 MessageCard 后使用 Bottom Sheet / Detail Drawer 显示同一套详情组件。
- Composer 保持在主内容区，不固定在虚拟键盘上方。
- MessageCard 仍有直接复制按钮。
- 底部主导航：临时区、长期区、搜索、上传、我的。
- “我的”次级菜单：标签、收藏、回收站、设备与会话、管理（管理员可见）、退出登录。

## 2. 非目标与硬性 Guardrails

Codex **不得** 因为 UI 重构而：

- 修改消息业务语义。
- 修改数据库或迁移。
- 修改上传协议、分片上传、恢复 ledger、reconcile 行为。
- 修改 Sensitive reveal 的安全模型。
- 将 Sensitive 明文写入 Pinia、localStorage、sessionStorage、URL 或持久化 Query cache。
- 修改 CSRF、认证、TOTP、会话撤销语义。
- 修改 Message mutation 的 expected-version / 并发语义。
- 手工编辑 `web/src/api/generated/*`。
- 为了原型中的 UI 假造后端统计数据。
- 将 session/device 数量直接宣称为“在线设备数”，除非已有真实 presence authority。
- 在普通 cursor Feed 上做客户端分页后 type-filter；这会破坏分页语义。
- 删除 `/messages/:id` 兼容入口。
- 大规模重写 `UploadManager`。

如果某个视觉要求必须新增后端/OpenAPI 能力才能正确完成，该子功能应标记 **DESIGN GAP**，先完成其余 UI，不得用错误客户端逻辑伪造完成状态。

## 3. 状态所有权最终目标

```text
Router
  page / route
  detail=<message-id>

TanStack Query
  feeds / detail / search / tags / sessions / admin / storage

Pinia Auth
  user / device / session / csrf

UploadManager + upload domain state
  upload lifecycle / progress / resume / ledger

UI Store
  themeMode
  uploadQueueOpen
  sessionsOpen
  mobileMoreOpen

Composer local/controller state
  draft / mode / lifecycle / tags / selected uploads / sensitive / direct recipient

Detail local/controller state
  sensitive revealed body / editing / attachment selection / forward form / notice/error
```

`selectedMessageId` 不进入 Pinia，以 URL query `detail=<id>` 为 canonical selection state。

---

# 4. Codex 上下文控制规则

本计划按 **6 个 Codex 任务**拆分。每个任务目标是能在单个 256K context 中完成；不要把整个仓库全部读入上下文。

每个任务开始时：

1. `git status --short`，确认工作区状态。
2. `git log -1 --oneline`，确认从上一任务 checkpoint 继续。
3. 只读取该任务“主要阅读范围”中的文件；需要依赖时再定点扩展。
4. 优先通过现有测试理解行为，不要为了 UI 重构阅读无关 Go/domain/backend 代码。
5. 不要把 `web/src/api/generated/` 整个目录加载到上下文；只按具体类型按需读取。
6. 遇到大文件先读取 script / template / 相关测试的必要区段，不要无目的重复读取。
7. 使用 `git diff --stat` / `git diff -- <paths>` 聚焦当前任务修改。

任务间允许存在“视觉尚未完整”“新组件尚未全部接线”的阶段状态。**不要求每个 checkpoint 都是最终可发布产品**。但每个任务必须保持自己负责的业务语义正确，不得以临时代码破坏安全/上传数据语义。最终可运行性和完整验证由 Task 6 收口。

每个任务完成后建议创建一个 checkpoint commit，并在交接摘要里写：

```text
Task:
Commit:
Changed:
Known temporary gaps:
Tests run:
Next task assumptions:
```

不要在交接摘要中复制巨量 diff。

---

# 5. Task 1 — Design System + Application Shell Foundation

## 目标

建立新的主题系统、桌面/移动 Shell 骨架和纯 UI state，为后续 Detail / Composer / Feed 重构提供稳定挂载点。此任务 **不重写消息内容组件**。

## 主要阅读范围

优先只读：

```text
web/src/main.ts
web/src/App.vue
web/src/app/AppShell.vue
web/src/app/router.ts
web/src/app/styles/tokens.css
web/src/app/styles/base.css
web/src/app/styles/layout.css
web/src/features/auth/store.ts
web/src/features/uploads/store.ts
web/src/features/uploads/components/UploadQueue.vue
web/src/features/sessions/SessionsPanel.vue
web/src/app/PWAUpdatePrompt.vue
相关 AppShell / router tests
```

必要时读取 `package.json` / Vite 配置，但不要扫描整个 backend。

## 实施内容

### 1. Theme foundation

建立 semantic tokens，至少包括：

```text
--surface-base
--surface-raised
--surface-soft
--surface-overlay
--text-primary
--text-secondary
--text-tertiary
--border-default
--border-strong
--accent-primary
--accent-primary-hover
--accent-primary-soft
--accent-secondary
--state-success
--state-warning
--state-danger
--state-info
--content-code
--content-image
--content-document
--content-archive
--shadow-sm
--shadow-md
--shadow-floating
```

配色方向：

- primary：violet / indigo
- secondary：teal
- info：blue
- success：green
- warning：amber
- danger：red/coral

禁止把所有 UI 都做成单一紫色。

实现 `system | light | dark`：

```text
web/src/app/composables/useTheme.ts
web/src/app/shell/ThemeToggle.vue
```

主题偏好可写 localStorage，但只能保存主题枚举。Vue mount 前初始化 `document.documentElement.dataset.theme`，避免 dark mode reload 白闪；system 模式监听 `matchMedia('(prefers-color-scheme: dark)')`。

### 2. UI store

新增轻量 UI store，例如：

```text
web/src/app/stores/ui.ts
```

仅包含：

```ts
themeMode
uploadQueueOpen
sessionsOpen
mobileMoreOpen
```

不要把 server data、selectedMessageId、Composer draft、Sensitive body 放进去。

`uploadState.queueOpen` 可在本任务或后续兼容迁移到 UI store，但不要重写 UploadManager。

### 3. Shell decomposition

将 AppShell 拆为类似：

```text
web/src/app/shell/AppSidebar.vue
web/src/app/shell/AppTopbar.vue
web/src/app/shell/MobileHeader.vue
web/src/app/shell/MobileBottomNav.vue
web/src/app/shell/MobileMoreMenu.vue
web/src/app/shell/SidebarStatusCard.vue
web/src/app/shell/AccountButton.vue
web/src/app/shell/ThemeToggle.vue
```

AppShell 只保留：

- shell composition
- auth/realtime bootstrap
- RouterView
- overlay mount points
- upload queue / sessions / PWA update integration

移除旧的顶部 Temporary/Permanent primary nav；Temporary/Permanent 改为 Sidebar 主导航。

### 4. Layout skeleton

Desktop 目标：

```css
grid-template-columns:
  220px
  minmax(560px, 1fr)
  clamp(400px, 31vw, 480px);
```

本任务可先给第三栏放空 Detail placeholder，不要求详情逻辑完成。

建议 desktop breakpoint 约 `1180px`；小于该值切换移动 Shell，不要强行压缩三栏。

### 5. Topbar

只保留：

- Global Search
- sync/realtime state（如果已有可靠语义）
- theme toggle
- 可选 account shortcut

不要显示 storage/device duplicate。

## 本任务明确不做

- 不抽 `MessageDetailView.vue` controller。
- 不重构 `MessageComposer.vue`。
- 不重构 `MessageCard.vue` / attachments。
- 不新增 Feed type filter。
- 不重做 Admin 内部业务页面。

## 建议修改/新增范围

```text
web/src/main.ts
web/src/app/AppShell.vue
web/src/app/router.ts（仅 shell/meta 必需项）
web/src/app/styles/*
web/src/app/stores/ui.ts
web/src/app/composables/useTheme.ts
web/src/app/shell/*
相关 tests
```

## Task 1 完成条件

- 新 Theme foundation 存在。
- Light / Dark / System 有明确实现路径。
- 新 Desktop 三栏 Shell 骨架存在。
- Mobile header / bottom-nav 骨架存在。
- 旧顶部 Temporary/Permanent nav 被 shell 设计取代。
- AppShell 不再承担大量导航 DOM。
- 现有业务组件可以暂时以旧外观挂在新 Shell 内。
- 不要求最终 Detail / Composer / Feed 外观完成。

---

# 6. Task 2 — Detail Selection + Inspector Architecture

## 目标

把当前大型 modal-style Message Detail 重构为可复用的 Detail Controller + Inspector，并实现：

- Desktop 第三栏 Detail Inspector
- Mobile Bottom Sheet
- URL query selection
- `/messages/:id` legacy/deep-link 兼容

这是一个高风险任务，应单独占用一个 Codex context。

## 主要阅读范围

```text
web/src/app/router.ts
web/src/app/AppShell.vue
web/src/app/shell/*（Task 1 产物）
web/src/features/messages/views/MessageDetailView.vue
web/src/features/messages/queries.ts
web/src/features/messages/mutations.ts
web/src/features/messages/components/AttachmentList.vue
web/src/features/messages/components/AttachmentSummary.vue
web/src/features/messages/components/SafeMarkdown.vue
web/src/features/files/AttachmentViewer.vue
web/src/features/uploads/manager.ts
web/src/features/uploads/store.ts
web/src/features/uploads/types.ts
web/src/features/tags/queries.ts
MessageDetail 相关 tests
```

按使用到的 API model 定点读取 generated type，不要全读 generated 目录。

## 实施内容

### 1. URL selection

新增：

```text
web/src/app/composables/useDetailSelection.ts
```

Canonical URL：

```text
/temporary?detail=<message-id>
/permanent?detail=<message-id>
/favorites?detail=<message-id>
/tags/<id>?detail=<message-id>
/search?q=nginx&detail=<message-id>
```

接口至少包含：

```ts
selectedMessageId
openDetail(id)
closeDetail()
```

修改 query 时必须保留其他 query 参数。浏览器 Back/Forward 应可控制详情开关。

### 2. Extract detail controller

从 `MessageDetailView.vue` 提取：

```text
web/src/features/messages/composables/useMessageDetailController.ts
```

必须保留现有：

- `useMessageDetail`
- Sensitive reveal request/version 防护
- reveal 明文仅内存存活
- edit body / body format
- tags update
- add/remove attachment
- completed uploads 恢复
- forward
- mutation invalidation
- attachment viewer query handling
- trash/delete/restore 语义
- error/notice

不要改变现有业务 API 调用序列。

### 3. Inspector components

建议：

```text
components/detail/MessageDetailSurface.vue
components/detail/MessageInspector.vue
components/detail/MessageDetailHeader.vue
components/detail/MessageDetailActions.vue
components/detail/MessageDetailMetadata.vue
components/detail/MessageLifecycle.vue
components/detail/MessageAttachmentSection.vue
```

不要为了“组件化”把无逻辑 wrapper 全拆文件。

### 4. Surface reuse

Desktop：`MessageDetailSurface` 渲染到第三栏。

Mobile：同一个 `MessageInspector` 渲染为 bottom sheet + scrim + drag handle。

`/messages/:id` 继续保留，但内部复用 `MessageInspector`；不能维护第二套 detail business implementation。

### 5. Empty state

Desktop 未选择内容时第三栏显示：

```text
选择一条内容查看详情
```

不要预取全部 message detail；只 query 当前 `selectedMessageId`。

## 安全硬约束

- Sensitive plaintext 不可进入 Pinia/localStorage/sessionStorage/URL。
- Sensitive body version change 后必须失效。
- Attachment viewer 安全行为保持不变。
- expectedVersion 语义保持不变。

## 本任务明确不做

- 不重构 Composer。
- 不大改 MessageCard visual；如果为调试 Detail 需要临时入口，只做最小接线，正式 card selection 留给 Task 4。
- 不实现 Feed type tabs。

## Task 2 完成条件

- Detail controller 已从原大型 View 抽离。
- Desktop 有第三栏 Detail Surface。
- Mobile 有同源 Bottom Sheet surface。
- URL `detail=` selection 工作模型清晰。
- `/messages/:id` 仍然兼容并复用 Inspector。
- Sensitive / edit / attachment / forward 业务语义没有被重写。
- 阶段性 MessageCard 仍可保留旧样式。

---

# 7. Task 3 — Composer Controller + Floating Composer UI

## 目标

单独重构当前大型 `MessageComposer.vue`。保留所有发送/上传语义，把默认 UI 压缩为第二栏顶部的悬浮 Composer，高级能力收进二级控制。

## 主要阅读范围

```text
web/src/features/messages/components/MessageComposer.vue
web/src/features/messages/components/MessageComposer.test.ts
web/src/features/messages/views/FeedView.vue
web/src/features/messages/queries.ts
web/src/features/tags/queries.ts
web/src/features/uploads/manager.ts
web/src/features/uploads/store.ts
web/src/features/uploads/types.ts
web/src/shared/utils/bytes.ts
相关 query keys / errors helper
Task 1 的 shell/styles
```

只按需要读取 CreateMessage/DirectSend/Lifecycle/BodyFormat 等 generated types。

## 实施内容

### 1. Extract controller

新增：

```text
web/src/features/messages/composables/useMessageComposer.ts
```

保留并暴露：

```text
body
mode
lifecycle
sensitive
selectedTags
selectedUploadClients / selectedUploads
directRecipient / directMode
byteLength / tooLarge
attachmentsBlocking / hasContent
dragging
error / failed / sending
selectFiles()
removeSelected()
pasteFiles()
dropFiles()
addTag()
addRestored()
submit()
submitDirect()
```

必须保持：

- Text / Markdown / Code
- Temporary / Permanent
- Sensitive
- Tag selection / create tag
- Direct Send
- 文件选择
- Drag & Drop
- 粘贴图片
- upload blocking
- restored completed upload
- body 1 MiB 限制
- idempotency key
- retry identity/fingerprint 逻辑
- Ctrl/Cmd + Enter
- 成功后的 query invalidation
- Composer 自己拥有 selected upload selection，UploadManager 只拥有 upload lifecycle

### 2. New UI composition

建议：

```text
MessageComposer
├── ComposerEditor
├── ComposerAttachments
└── ComposerToolbar
    ├── FileButton
    ├── TagPicker
    ├── LifecycleMenu
    ├── AdvancedMenu
    └── SendButton
```

AdvancedMenu 可放：

- Text / Markdown / Code
- Sensitive
- Direct send recipient
- 创建标签
- restored uploads

默认可见只保留高频操作：

```text
正文
文件
标签
生命周期
发送
```

### 3. Placement

Temporary / Permanent 页：Composer 位于第二栏 Feed 顶部。

Favorites / Tag / Trash：无 Composer。

Desktop：

```css
.composer-shell {
  width: min(84%, 760px);
  margin-inline: auto;
}
```

实际值可按现有 layout 微调，但必须保留左右空白和 floating-card 感。

Mobile：Composer 位于主内容流顶部，不 fixed 到 viewport bottom。

## 本任务明确不做

- 不重做 Detail controller。
- 不重做 MessageCard / attachment feed presentation。
- 不修改 UploadManager 核心。
- 不改变 API payload 语义。

## Task 3 完成条件

- Composer business logic 从 View 层明显抽离。
- 新 Composer 在第二栏顶部，视觉不占满工作区。
- Mobile Composer 同样位于 main flow。
- 现有 Composer 行为测试应尽量迁移/保留，而不是删除。
- 如果阶段性 Feed/Card 仍是旧视觉是允许的。

---

# 8. Task 4 — Feed, Message Cards, Quick Copy, Rich Attachments

## 目标

完成第二栏的信息浏览体验：新 Feed、卡片、selected state、直接复制、附件视觉。此任务把 Task 2 的 Detail selection 正式接到 MessageCard。

## 主要阅读范围

```text
web/src/features/messages/components/MessageFeed.vue
web/src/features/messages/components/MessageCard.vue
web/src/features/messages/components/MessageCard.test.ts
web/src/features/messages/components/AttachmentList.vue
web/src/features/messages/components/AttachmentSummary.vue
web/src/features/messages/views/FeedView.vue
web/src/features/messages/queries.ts
web/src/features/messages/mutations.ts
web/src/shared/ui/TagChip.vue
Task 2 useDetailSelection / Detail surface
Task 3 MessageComposer public surface
相关 tests
```

## 实施内容

### 1. MessageCard structure

建议最终结构：

```text
MessageCard
├── Header
│   ├── TypeBadge
│   ├── title / first line
│   └── ExpiryBadge
├── MessagePreview
├── AttachmentGrid / AttachmentPreview
├── MessageMeta
│   ├── source/time
│   └── tags
└── QuickCopyButton
```

视觉要求：

- Card 密度高于传统大面板，但保持可扫描。
- Selected card 有明确但不过度的 accent 边框/背景。
- Code preview 使用 mono/code semantic surface。
- 图片和文件附件有不同 visual treatment。
- 不制造无意义的强 glow / 电竞 dashboard 风格。

### 2. Quick Copy

新增独立 `QuickCopyButton.vue` 或等价逻辑组件。

必须：

```ts
event.stopPropagation()
```

行为：

```text
normal + full body preview -> copy
sensitive -> open detail
bodyTruncated -> open detail
no body -> 不显示正文复制按钮
```

不能把附件文件名当作“正文复制”的替代。

### 3. Card click

Card click：

```text
openDetail(message.id)
```

即更新 `?detail=`，不要再从 Feed 强制跳转 `/messages/:id`。

### 4. Attachment presentation

保留现有 thumbnail endpoint、retry、safe image MIME、lazy loading 和 viewer 行为。

可新增：

```text
components/attachments/AttachmentCard.vue
components/attachments/AttachmentGrid.vue
components/attachments/AttachmentIcon.vue
components/attachments/AttachmentThumbnail.vue
```

图片：明显 thumbnail。
PDF/Text/Archive/APK 等：文件卡片 + semantic icon/label。

扩展名只能增强视觉，不能决定安全 preview 能力；server detected MIME 仍是 authority。

### 5. Feed toolbar

原型里的：

```text
全部 / 文本 / 命令 / 文件 / 图片
```

**不能** 在普通 cursor feed 上通过 `items.filter(...)` 实现。

如果当前 `listMessages()` 仍没有 server-backed type filter：

- 第一阶段隐藏 type tabs；或
- 展示 disabled / future affordance；或
- 只提供当前 API 真正支持的 filter。

不要为了 UI 完整感破坏无限分页。

## 本任务明确不做

- 不新增后端 type filter。
- 不重写 MessageFeed infinite-query。
- 不重做 Admin。
- 不重复实现 Detail business logic。

## Task 4 完成条件

- MessageCard 新视觉完成。
- Card click 打开 Detail Surface。
- Quick Copy 不打开 Detail。
- Sensitive / truncated 不被绕过。
- Rich attachment feed presentation 完成。
- Infinite query / pagination 保持原逻辑。
- FeedToolbar 不使用错误客户端分页后过滤。

---

# 9. Task 5 — Shell Integration, Status, Mobile Navigation, Route Compatibility

## 目标

把前四个任务的 UI pieces 连接成完整产品结构，处理状态区、移动导航、UploadQueue/Sessions、Admin/Search/Favorites/Tags/Trash 等跨页面兼容问题。

## 主要阅读范围

```text
web/src/app/AppShell.vue
web/src/app/router.ts
web/src/app/shell/*
web/src/app/stores/ui.ts
web/src/app/realtime.ts
web/src/features/auth/store.ts
web/src/features/sessions/SessionsPanel.vue
web/src/features/uploads/components/UploadQueue.vue
web/src/features/uploads/store.ts
web/src/features/admin/AdminView.vue
web/src/features/search/SearchView.vue
web/src/features/messages/views/FeedView.vue
web/src/features/tags/queries.ts
相关 shell/router/admin/search tests
```

如果需要复用 admin storage/status query，再读取 Admin query keys/API 调用；不要重新阅读 Task 2/3 的大业务文件，除非出现明确 integration bug。

## 实施内容

### 1. Sidebar final navigation

Desktop 左栏：

```text
临时区
长期区
收藏
搜索
标签

上传
回收站

管理面板 (admin only)

SidebarStatusCard
Account
```

### 2. Sidebar status single source

最终 storage/status 只显示在 SidebarStatusCard。

Topbar 不再重复：

- storage usage
- device count

如果 storage endpoint 为 admin-only：

```text
auth.user?.isAdmin
```

才启用对应 query；非管理员不能产生无意义 403。

存储数值必须使用真实 API，例如 logical usage/max storage/NAS status，不得硬编码原型数据。

### 3. Device / presence semantics

已有 `listDevices()` / `listSessions()` 并不天然代表 online presence。

如果没有真实 presence authority，不显示：

```text
3 台设备在线
```

改为事实性文案，例如：

```text
3 个设备
实时连接正常
```

或只显示 realtime connected 状态。

### 4. Mobile bottom nav

固定：

```text
临时区
长期区
搜索
上传
我的
```

“我的”打开 MobileMoreMenu：

```text
标签
收藏
回收站
设备与会话
管理（admin only）
退出登录
```

处理 iPhone safe area：

```css
padding-bottom: calc(... + env(safe-area-inset-bottom));
```

### 5. Upload / Sessions

- Upload item / queue domain lifecycle 保持原样。
- UI store 管理 queue drawer 是否打开。
- SessionsPanel 继续保留 TOTP、rename device、revoke session、change password 等能力。
- 可以统一视觉，但不要把这些安全功能删掉或简化掉。

### 6. Admin layout

Admin 不强制显示 Feed 第三栏。

建议 route meta 或 shell mode：

```text
workspace -> sidebar + feed + detail
full      -> sidebar + full main workspace
```

Admin 使用 `full`。

本任务只统一 Admin 与新 token/button/panel/form 视觉，除非必要不要重写 Admin 业务。

### 7. Route compatibility

验证：

```text
/temporary
/permanent
/favorites
/tags/:id
/trash
/search?q=...
/messages/:id
/admin/...
```

Search/Favorites/Tags/Trash 也应能使用 `?detail=id` 打开 Detail Surface（Trash action 语义按现有逻辑）。

## Task 5 完成条件

- Desktop shell 功能接线完整。
- 存储/设备信息不重复。
- Mobile A 导航完整。
- UploadQueue、SessionsPanel、Account、Logout 正常挂载。
- Admin 使用合适 shell layout。
- Search/Favorites/Tags/Trash 与 Detail selection 兼容。
- 没有把 session count 错称为 online presence。

---

# 10. Task 6 — Integration QA, Responsive Polish, Tests, Cleanup

## 目标

最后一次 Codex context 专门用于跨任务收口。此任务不再主动设计新架构；以修复 integration、补测试、清理旧代码、完整验证为主。

## 上下文策略

不要重新通读整个仓库。

开始时使用：

```text
git log --oneline <UI-start>..HEAD
git diff <UI-start>..HEAD --stat
```

只按失败测试、lint、typecheck、E2E、视觉/响应式问题定点读取文件。

主要关注：

```text
web/src/app/**
web/src/features/messages/**
web/src/features/search/**
web/src/features/admin/**（仅 integration）
web/src/features/sessions/**（仅 integration）
web/src/features/uploads/**（仅 integration）
web/src/test/**
Playwright / e2e files
```

不要因前端测试失败扩散重构后端。

## 必须验证的行为

### Theme

- system 默认模式。
- 切 light。
- 切 dark。
- preference persistence。
- system preference change。
- reload 无明显 white flash。
- dialog/bottom-sheet 在两种主题可读。

### Desktop shell

- 三栏在桌面阈值正常。
- Composer 位于第二栏且左右留白。
- 第三栏全部用于 Detail。
- 未选中有 empty state。
- Storage/status 只出现一处。
- Topbar 无重复 storage/device。

### MessageCard

- click card -> detail query / inspector。
- copy button -> 不打开 detail。
- normal body copies。
- truncated -> 打开 detail。
- sensitive -> 打开 detail/reveal flow。
- selected visual state 正常。

### Detail

- `?detail=id` reload 可恢复。
- close 删除 `detail` 但保留其他 query。
- Browser Back/Forward 合理。
- Search query + detail 共存。
- `/messages/:id` deep link 仍可用。
- Sensitive reveal/edit/version protection 正常。
- Attachment add/remove/view 正常。
- Forward 正常。

### Composer

- Text/Markdown/Code。
- Temporary/Permanent。
- Sensitive。
- Tags/create tag。
- Direct Send。
- file choose。
- drag/drop。
- paste image。
- restored uploads。
- retry/idempotency。
- 1 MiB body limit。
- Ctrl/Cmd+Enter。
- upload pending/failed blocking。

### Mobile

至少覆盖：

```text
390x844
393x852
430x932
768x1024
```

检查：

- desktop sidebar/detail 固定栏隐藏。
- bottom nav 出现。
- Composer 在 main flow。
- card direct copy 不打开 sheet。
- tap card 打开 Bottom Sheet。
- My 打开 secondary menu。
- safe-area 正常。
- touch target 约 42–44px 以上。
- virtual keyboard 不依赖 fixed composer。

### Accessibility

- keyboard navigation。
- visible focus ring。
- aria-current / aria-label。
- dialog semantics。
- Esc close。
- `prefers-reduced-motion`。
- 状态不只靠颜色表达。

## 删除/清理

删除已经确认无引用的：

- 旧 primary Temporary/Permanent nav CSS/markup。
- 旧 duplicate storage/device UI。
- 被新 Detail Surface 替代且不再使用的 modal-only CSS。
- duplicate media queries。
- dead shell components。
- dead temporary adapters。

不要删除仍承担 legacy route / security / upload compatibility 的代码。

## 最终验证命令

最终 Task 6 必须以仓库现有权威命令为准运行：

```bash
make lint
make test
make e2e
make build
```

如果 `make e2e` 需要外部依赖/浏览器且当前环境无法满足，必须明确记录阻塞原因；其余可运行验证仍要完成。

## 最终 Definition of Done

- [ ] Desktop 三栏 Power User layout。
- [ ] Composer 在第二栏、左右留白、floating-card 视觉。
- [ ] 第三栏全部 Detail Inspector。
- [ ] MessageCard 直接复制。
- [ ] Quick Copy 不打开 Detail。
- [ ] Sensitive/truncated 不绕过安全边界。
- [ ] Rich attachment presentation。
- [ ] Light / Dark / System。
- [ ] Theme preference persistence。
- [ ] Storage/device/status area 仅一处。
- [ ] Mobile 使用方案 A 底栏导航。
- [ ] Mobile Composer 在 main feed。
- [ ] Mobile Detail 为 Bottom Sheet。
- [ ] Mobile Quick Copy 不打开 Bottom Sheet。
- [ ] Admin/Search/Tags/Favorites/Trash 正常。
- [ ] Upload resume/retry/reconcile 正常。
- [ ] Sensitive reveal/edit 正常。
- [ ] Direct Send / Forward 正常。
- [ ] Attachment add/remove/view 正常。
- [ ] Existing unit tests 保留并通过。
- [ ] 新 UI 行为 tests 完成。
- [ ] Desktop/mobile E2E journeys 完成。
- [ ] `make lint` 通过。
- [ ] `make test` 通过。
- [ ] `make e2e` 通过或有明确环境阻塞记录。
- [ ] `make build` 通过。

---

# 11. 六个任务的依赖关系

```text
Task 1  Theme + Shell foundation
   │
   ├───────────────┐
   │               │
   ▼               ▼
Task 2 Detail     Task 3 Composer
   │               │
   └───────┬───────┘
           ▼
Task 4 Feed / Cards / Attachments
           │
           ▼
Task 5 Shell integration / Mobile / Status / Routes
           │
           ▼
Task 6 Final integration / QA / Cleanup
```

推荐实际串行执行 `1 -> 2 -> 3 -> 4 -> 5 -> 6`。虽然 Task 2/3 概念上部分独立，但不要并行修改同一个 branch，避免 Shell/FeedView 冲突。

---

# 12. 每个 Codex 会话可直接使用的启动模板

每次只把当前 Task 的章节作为主任务，并附以下固定提示：

```text
You are implementing one task from docs/UI_REFACTOR_EXECUTION_PLAN.md.

Authority:
- Start from the current repository HEAD. Do not reset to the analysis baseline written in the document.
- Treat the previous UI-refactor task commit as accepted input.
- Follow only the current task scope plus the global guardrails in sections 1-4.

Context budget:
- You have a 256K context window. Do not load the whole repository.
- Read the files listed under this task's "主要阅读范围" first.
- Expand only when a concrete dependency or failing test requires it.
- Do not read generated API directories wholesale.

Execution:
- Preserve existing business/security/upload semantics.
- Do not invent backend data to satisfy a mockup.
- Intermediate task checkpoints do not need to be release-ready; the final integrated state after Task 6 must be correct.
- Add/update tests that directly protect behavior changed by this task.
- At the end provide: changed files, tests run, known temporary gaps, and assumptions for the next task.
```

Task 6 再附加：

```text
This is the final integration task. Temporary gaps from Tasks 1-5 are no longer acceptable unless they are a documented external-environment blocker. Run the full available validation suite and remove obsolete compatibility scaffolding that is no longer needed.
```

---

# 13. 决策优先级

如果实现过程中 UI 原型与已有可靠性/安全语义冲突，优先级固定为：

```text
业务正确性
> 安全性
> 上传/恢复可靠性
> 数据与分页正确性
> 响应式可用性
> 视觉一致性
```

本轮重构成功的标准不是“看起来像原型图”，而是：在保留 RelayShelf 现有可靠业务能力的前提下，把主要工作流变为：

```text
快速投递
  ↓
浏览内容
  ↓
一键复制
  ↓
必要时查看详情
  ↓
转长期 / 收藏 / 分享 / 管理附件
```

桌面端强调信息密度和不打断上下文；移动端强调单手导航、快速投递、快速复制和按需详情。