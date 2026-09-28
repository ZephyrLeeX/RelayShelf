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
