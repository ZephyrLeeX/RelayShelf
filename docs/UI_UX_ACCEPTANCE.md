# UI/UX 验收记录（2026-09-28）

## 实现

- 管理员重置密码：显示明确成功/失败通知，提交中禁用确认与取消，显示“处理中…”，防止重复提交。
- 全局通知：顶部居中、按产生顺序纵向堆叠，保留成功/错误/警告/普通消息、超时移除和手动关闭；移动端保留边距。
- 图片：消息卡片和详情共用大图预览，支持关闭按钮、遮罩、Esc、方向键、上一张/下一张、缩放、附件下载和失败重试；从卡片打开时读取完整消息，超过三张附件也可切换。
- 富文本：发送与编辑共用 Markdown 格式工具栏和预览，支持加粗、斜体、列表、链接、引用、代码块。复用 TEXT/MARKDOWN 存储和 DOMPurify 安全渲染；Enter 换行，Ctrl/⌘+Enter 发送或保存，中文输入法组合过程中不提交。
- 标签：发送面板与消息标签面板均支持外部点击和 Esc 关闭；内部交互保留；优化选中、悬停、焦点、禁用、加载、错误、空状态和移动布局。

## 本地验证

先检查 `git diff --name-only`，只运行下列相关验证。以下 Vitest 文件最终均 PASS，共 134 项（分批运行，修改后仅重跑受影响文件）：

- `web/src/features/admin/AdminView.test.ts`：5
- `web/src/shared/ui/ToastViewport.test.ts`：1
- `web/src/features/messages/components/MessageTagPicker.test.ts`：2
- `web/src/features/messages/components/composer/ComposerEditor.test.ts`：7
- `web/src/features/messages/components/ImagePreview.test.ts`：2
- `web/src/features/messages/components/MessageComposer.test.ts`：44
- `web/src/features/messages/components/detail/MessageDetailSurface.test.ts`：11
- `web/src/features/messages/components/SafeMarkdown.test.ts`：7
- `web/src/features/messages/components/MessageCard.test.ts`：29
- `web/src/features/messages/content/contentFormat.test.ts`：20
- `web/src/features/files/AttachmentViewer.test.ts`：6

其他验证：

- `go test ./internal/platform/httpx/...` PASS：图片 CSP 调整及其他安全头限制。
- `vue-tsc -b --pretty false` PASS。
- ESLint 仅检查修改与新增的 Vue/TS 文件，PASS；`git diff --check` PASS。
- `E2E_BASE_URL=http://127.0.0.1:5173 playwright test e2e/message-ux.spec.ts`：2 PASS，覆盖真实登录、上传四张图片、正文图片跨来源切换、标签关闭、IME、富文本发送/展示/再次编辑；1440×900 和 390×844。
- 隔离组件 Chromium 检查：1280×900、390×844 均 PASS，额外检查四类通知居中且不重叠、格式预览、遮罩关闭及移动标签面板边界。

未运行 `make dev-check`、`make test`、`make e2e`、`go test ./...`、全量前端 Vitest 或全量集成/E2E。完整回归由 GitHub CI 判定，本记录不表示 CI 已通过。

## Dev Preview

访问 http://localhost:5173 （绑定 0.0.0.0:5173），API 仅监听 127.0.0.1:8080。

`make dev-preview` 首次因 Go 缺失失败；补齐运行时后，Docker socket 权限阻止其启动数据库。最终使用 `.local/dev/postgres-runtime` 下的 PostgreSQL 17，在 `.local/dev/postgres-data` 创建独立实例，监听 127.0.0.1:55432，数据库和用户均为 `relayshelf_dev`。后端和 Vite 复用开发脚本的启动函数；日志、存储、暂存和开发密钥均位于 `.local/dev`，未读取生产 `.env`、数据库、NFS 或生产密钥。前端 HTTP 200、后端健康检查 PASS，真实 E2E 已登录验证。

当前原生数据库启动方式不同于 Docker，停止时除 `scripts/dev-stack.sh` 管理的后端/Vite 外，还需使用 `.local/dev/postgres-runtime/root/usr/lib/postgresql/17/bin/pg_ctl -D .local/dev/postgres-data stop`（设置对应运行时的 `LD_LIBRARY_PATH`）。环境专用启动脚本保留在 `.local/dev/start-native-backend.sh` 和 `.local/dev/start-native-vite.sh`，不纳入版本控制。

| 账号 | 密码 | 用途 |
| --- | --- | --- |
| e2e-alice | e2e-alice-pass-12345 | 普通用户 |
| e2e-bob | e2e-bob-pass-123456 | 第二个普通用户 |
| e2e-admin | e2e-admin-pass-12345 | 管理员 |

## 人工验收

1. 管理员进入用户管理，对测试用户重置密码：确认处理中按钮不可重复点击，成功后弹窗关闭且顶部显示明确提示；开发者工具阻断请求后确认失败提示、弹窗保留且可重试。建议新建临时用户验收，避免改变上述固定账号密码。
2. 连续复制、收藏、更新标签，检查通知顶部居中、垂直排列、不同类型颜色与图标；等待自动消失，并手动关闭一条。手机宽度下检查左右边距。
3. 发送至少四张图片附件及两个 Markdown 图片链接。点击卡片中的附件和正文图片链接，检查完整图片切换、比例、缩放、关闭按钮/遮罩/Esc；用不存在的图片地址检查失败与重试。
4. 发送和编辑时依次使用六种格式，预览、发送、打开详情、编辑保存、再次编辑，确认格式与内容保留。检查旧纯文本仍按原样显示；Enter 只换行，Ctrl/⌘+Enter 提交；中文选词时不提交。
5. 打开发送框标签面板和消息标签面板，分别点击内部输入、添加、选择、外部空白以及其他按钮；只有外部点击/Esc 收起，正常添加和选择操作保留。
6. 在桌面和 390px 手机宽度检查标签选中、hover、focus、禁用、加载/空状态，以及弹层宽度与可滚动区域。

## 限制

- 富文本采用 Markdown 源码工具栏与预览，并非所见即所得编辑器。
- 为保持已有敏感正文编辑接口约束，历史敏感 TEXT 消息仍保持纯文本；敏感 MARKDOWN 消息可使用格式工具栏，并仅在揭示后安全渲染。
- 远程正文图片默认不自动请求，点击预览才加载且不发送 Referer；远端防盗链、无效 URL、HTTPS 页面中的不安全 HTTP 图片仍可能无法显示。SVG 等活动附件维持原有仅下载策略。
- 未创建 tag、GitHub Release 或生产部署；未推送或声称 GitHub CI 已通过。

## 2026-09-30 图片预览统一验收

### 实现范围

- 卡片、消息详情、附件及外部 Markdown 图片沿用当前消息的图片集合；AttachmentViewer 的图片分支复用 ImagePreview，删除独立缩放/切图交互。PDF、文本、音视频及活动文件仅下载策略保留。
- 适应窗口按图片 naturalWidth/naturalHeight 与可用视口计算，不放大小图片；原始尺寸为 100%，百分比显示至 0.1%。支持按钮/滚轮缩放（最高 800%）、双击适应窗口/原始尺寸、边界内拖动、双指缩放；仅在适应窗口时单指横滑切图，双指操作和取消手势不会触发切图。
- 图片切换和重试重建当前图片节点并校验来源/版本，旧图片事件无法覆盖新状态；附件其他媒体的预检也忽略已取消请求。加载中显示加载状态，不显示仅下载提示。
- 弹层锁定背景滚动、限制键盘焦点、恢复触发元素与页面位置，适配安全区和尺寸变化。下载原图入口在手机上可见。
- 缩略图仍懒加载，附件 ID 变化重置缩略图重试。附件使用原有授权 URL，未修改 Go、数据库、OpenAPI、敏感正文边界或 Service Worker。外部 Markdown 图片继续主动点击加载、HTTP(S) 限制和 no-referrer 策略。

### 本地验证

测试前检查 `git diff --name-only`，只选择相关范围。

- `pnpm --dir web exec vitest run src/features/messages/components/ImagePreview.test.ts src/features/files/AttachmentViewer.test.ts src/features/messages/components/attachments/AttachmentThumbnail.test.ts src/features/messages/components/SafeMarkdown.test.ts src/features/messages/components/MessageCard.test.ts`：5 文件、49 项 PASS。
- `image-preview.spec.ts`：桌面 1440×900、移动 390×844（含 844×390 横屏），2 项 PASS。真实登录/上传/下载；验证卡片/详情/Markdown 入口、无原图预加载、百分比、桌面滚轮/拖动/双击/按键、CDP 触摸双指缩放/平移/切图、焦点及滚动恢复、加载/失败/重试。
- `pnpm --dir web exec vue-tsc -b --pretty false` PASS；对本任务 8 个 Vue/TypeScript 文件运行 ESLint PASS；`git diff --check` PASS。
- 未运行全量前端、Go、集成或 E2E，也未运行 `make dev-check`；全量回归留给 GitHub CI，本次局部通过不代表 CI 全量回归通过。未修改 OpenAPI，因此无需 `make generate`。

`make dev-preview` 因缺少 podman 无法直接启动，Docker socket 也不可访问；复用既有 `.local/dev/postgres-runtime` 的独立 PostgreSQL（127.0.0.1:55432，数据库 `relayshelf_dev`）及 `.local/dev/start-native-{backend,vite}.sh` 启动。Vite 在 **0.0.0.0:5173**，API 在 127.0.0.1:8080；两者健康检查 HTTP 200。未使用生产 `.env`、NFS、数据库或密钥。

本机 Chromium headless shell 崩溃，最终验证使用已安装的完整 Chromium、`.local/dev/browser-libs` 与专用字体配置；临时配置 `.local/dev/image-preview.playwright.config.ts` 仅匹配本 spec 并复用 5173，不启动默认 E2E 数据库：

```sh
FONTCONFIG_FILE="$PWD/.local/dev/browser-libs/fonts.conf" \
LD_LIBRARY_PATH="$PWD/.local/dev/browser-libs/root/usr/lib/x86_64-linux-gnu" \
pnpm --dir web exec playwright test --config ../.local/dev/image-preview.playwright.config.ts
```

### 人工验收与限制

实际登录验证账号：`e2e-alice` / `e2e-alice-pass-12345`，地址 `http://localhost:5173`。

1. 用 Markdown 类型发送两张大图片附件和一个图片链接，分别从卡片、详情附件及正文链接打开；检查图片计数、完整适配、原始尺寸 100%、缩放按钮/滚轮、双击、放大后拖动和方向键切图。
2. 手机真机检查双指缩放与抬起一指后的拖动；回到适应窗口后左右滑动，确认放大拖动不会误切图。检查刘海/底部安全区、横竖屏和浏览器地址栏收缩时的控件可见性。
3. 点击下载原图；关闭按钮/Esc 关闭后确认触发元素焦点、原页面位置及详情面板仍保留，Tab 不进入背景。
4. 用无法访问的外部图片观察加载、失败、重试；快速切换图片，确认不会出现旧图加载状态或仅下载提示。

真实 iOS/Android 触屏手感、系统手势和浏览器动态工具栏没有通过桌面 Chromium 模拟完成真机验证，仍需人工验收。远端防盗链、失效链接及混合内容仍按原有安全策略失败。
