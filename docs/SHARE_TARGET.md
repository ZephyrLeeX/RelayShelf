# 系统分享接收（文本与链接）

## 已有能力与范围

沿用 Vue 编辑器、Go Session / CSRF / Origin 检查和 owner-authorized 消息发送。
没有新增后端接口、数据库表或 OpenAPI 改动；附件授权、敏感正文加密和生命周期不变。
接收 title / text / url，不接收文件，不抓取网页正文或元数据。

分享先暂存，点击「带入编辑器」后检查内容、接收人、保存位置与敏感设置，再点击现有「发送」。
已有标题、正文或选中附件时提供「合并到草稿」与「替换标题和正文」，替换还需确认。
替换仅替换标题、正文并切换为纯文本；附件、敏感设置、标签、接收人与保存位置保留。
合并保留原格式，将分享标题与正文附加到原草稿。分享与发送中草稿不会自动互相覆盖。
manifest 明确使用 launch_handler.navigate-new（[Chrome 官方说明](https://developer.chrome.com/docs/web-platform/launch-handler)），以保护原窗口草稿。编辑器有草稿时还注册 beforeunload 提醒；移动系统可能忽略该提醒，旧浏览器也可能忽略 launch_handler，需真实设备核实窗口行为。新分享窗口不合并其他窗口的内存草稿，请保留原窗口；只有当前编辑器内的草稿参加合并选择。

## 平台核实（2026-09-30）

- [Chrome 官方文档](https://developer.chrome.com/docs/capabilities/web-apis/web-share-target)：Android Chrome 76+、桌面 Chrome 89+ 有该 API；只有安装后的 PWA 才能成为分享目标。Android 常将 URL 放在 text，url 字段可能为空。
- [Microsoft Edge 官方文档](https://learn.microsoft.com/en-us/microsoft-edge/progressive-web-apps/how-to/share)：安装后的 PWA 可接收 Windows 系统分享；支持 POST 和 Service Worker 拦截。
- [MDN 兼容性数据](https://github.com/mdn/browser-compat-data/blob/main/manifests/webapp/share_target.json)目前记录 Safari / iOS、Firefox 不支持；[WebKit 跟踪项](https://bugs.webkit.org/show_bug.cgi?id=194593)。桌面浏览器解析 manifest 不代表所有 OS 的系统菜单都集成该能力，需在目标设备验收。

无需也无法通过 navigator.share / canShare 判断分享「接收」支持，它们检测的是发送。
界面没有伪造的系统分享按钮，提供简短安装说明与复制粘贴替代方法。
manifest 中不支持的成员由浏览器忽略。真实设备安装应使用可信 HTTPS，手机局域网 HTTP 的 Vite 页面不能验证 PWA 分享。

## 数据边界与恢复

manifest 使用 POST multipart/form-data，因此无需以查询参数交付正文。
Service Worker 只拦截同源 /share POST，在内存中解析，最多 1 MiB + 16 KiB 的请求编码开销，拒绝文件、未知及重复字段。
标题上限 200 字符，最终正文 UTF-8 上限 1 MiB，超限不静默截断。
Service Worker 不访问 API、不保存正文到 Cache / LocalStorage / IndexedDB、不记录请求正文。
303 跳转仅带随机一次性 token 的 fragment；fragment 不随 HTTP 请求或 Referer 发出。页面领取前立即移除 fragment，登录重定向只有 /share。
领取校验同源 /share 页面、目标客户端（浏览器提供 resultingClientId 时）及随机 token；一次领取后销毁。最多暂存 8 条，交接期限 60 秒。
浏览器、操作系统及同源脚本仍可在客户端内存访问数据；这不是对受控浏览器或同源 XSS 的加密隔离。

未登录时在认证路由保护执行前领取，正文留在当前页内存，经密码和 TOTP 登录后继续编辑。
登录失败或网络错误可在同页重试。退出已有账号时清除未处理分享，避免跨账号残留。
刷新、关闭页面或系统终止进程可能丢失正文，Worker 重启也可能导致交接失败；此时提示重新分享或复制粘贴。
这一限制遵循 PRD 的「未发送正文不持久化」规则。不能承诺跨刷新或跨进程恢复。
发送仍使用现有 CSRF / Origin / Session 校验；分享 POST 不写入服务端，没有放宽全局安全检查。
若 Service Worker 尚未安装/激活，不能保证接收，请先打开安装后的应用，或使用粘贴。
不支持 GET 正文交付；手工构造带正文的 URL 本身会暴露给历史记录/代理，应用不会将其当作分享解析。

## 定向验证

每次测试前先运行 git diff --name-only。

```sh
cd web
pnpm exec vitest run src/features/share/receive.test.ts src/features/share/worker.test.ts src/features/messages/components/MessageComposer.test.ts src/features/auth/store.test.ts src/app/router.test.ts
```

构建版浏览器测试前需要生成 web/dist：通常执行 pnpm build。
在现有 unrelated typecheck 错误阻断时，可仅 pnpm exec vite build 验证 PWA 构建，但不能称为类型检查通过。
浏览器流程需要构建版 SW，Vite HMR 5173 默认没有 SW：

```sh
make dev-preview
cd web
pnpm build
RELAY_SHELF_DEV_API_ORIGIN=http://127.0.0.1:8080 pnpm exec vite preview --host 127.0.0.1 --port 5175 --strictPort
# 在另一个终端，仍在 web/ 中：
pnpm exec playwright test --config playwright.share.config.ts
```

API 仅使用现有 .local/dev 及独立 relayshelf_dev 数据库。
测试使用 e2e-alice / e2e-alice-pass-12345 与 e2e-bob / e2e-bob-pass-123456，验证 POST 交接、未登录恢复、链接去重、确认前无消息请求及其他账号不可见。
操作系统分享菜单、安装注册和各平台窗口复用行为不由桌面 Playwright 验证。

## 人工与真实设备验收

1. 5173 页面用 Alice 登录，查看编辑器「从其他应用分享」说明；复制文本/链接粘贴，确认只有点击发送才产生消息。
2. Android Chrome 在可信 HTTPS 测试地址安装 PWA（既有安装可能需等待 manifest 更新或重新安装），打开一次以激活 SW。由浏览器/其他应用分享文字、只有链接、标题加文字加重复 URL，选 RelayShelf，确认显示接收提示、无自动发送，带入后 URL 仅一次。
3. 退出登录后重新从其他应用分享，检查地址与登录重定向中没有正文；错误密码后重试，正确登录后分享仍可带入。启用 TOTP 的账号也应完成同样验收。
4. 在当前编辑器输入原草稿并添加附件、启用敏感设置，再交付分享到当前应用（设备若新建窗口，保留并检查原窗口）。确认合并保留草稿；取消替换保持原文，确认替换仅改标题正文；检查附件、接收人、敏感设置后手动发送。
5. Windows Edge 安装相同测试 PWA，从支持 Windows 系统分享的应用选择 RelayShelf，重复步骤 2–4。不要以 Android 通过替代 Windows 验证。
6. iOS Safari、Firefox 检查复制粘贴说明，无不可用的接收按钮；使用复制粘贴发送。
7. 在登录页刷新或模拟 Worker 被终止，检查没有发送、提示重新分享；使用新的分享恢复。Bob 登录应看不到 Alice 已发送的消息。
