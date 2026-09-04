<script setup lang="ts">
import { CheckCircle2, ChevronDown, KeyRound, Laptop, Monitor, Pencil, ShieldCheck, Smartphone, X } from '@lucide/vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { DefaultService, type TOTPEnrollmentPending } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { apiCodes, displayError, toApiError } from '@/shared/api/errors'
import { useAuthStore } from '@/features/auth/store'
import { toast } from '@/shared/ui/toast'
import TotpEnrollmentQr from './TotpEnrollmentQr.vue'

const emit = defineEmits<{ close: [] }>()
const auth = useAuthStore()
const client = useQueryClient()
const name = ref(auth.device?.name ?? '')
const error = ref('')
const currentPassword = ref('')
const newPassword = ref('')
const drawer = ref<HTMLElement>()
const renaming = ref(false)
const passwordOpen = ref(false)
const totpOpen = ref(false)
let returnFocusTo: HTMLElement | null = null
const enrollmentPassword = ref('')
const totpCode = ref('')
const pendingEnrollment = ref<TOTPEnrollmentPending | null>(null)
const displayName = computed(() => auth.user?.displayName || auth.user?.username || 'RelayShelf')
const username = computed(() => auth.user?.username ? `@${auth.user.username}` : '已登录账号')
const avatarText = computed(() => displayName.value.slice(0, 1).toUpperCase())
const totp = useQuery({ queryKey: ['auth', 'totp'], queryFn: () => DefaultService.getTotpStatus() })
const totpEnabled = computed(() => totp.data.value?.enabled === true)

function showError(cause: unknown, fallback?: string) {
  error.value = fallback ?? displayError(cause)
  toast.error(error.value)
}
function clearError() { error.value = '' }
function formatLastSeen(value: string) { return new Date(value).toLocaleString() }
function sessionIcon(deviceId: string) {
  const agent = devices.data.value?.find((item) => item.id === deviceId)?.userAgent?.toLowerCase() ?? ''
  return /android|iphone|ipad|mobile/.test(agent) ? Smartphone : Monitor
}

const enroll = useMutation({
  mutationFn: () => DefaultService.startTotpEnrollment({ currentPassword: enrollmentPassword.value }),
  onSuccess: (data) => { clearError(); enrollmentPassword.value = ''; pendingEnrollment.value = data; totpCode.value = '' },
  onError: (cause) => { enrollmentPassword.value = ''; showError(cause) },
})
const confirmEnrollment = useMutation({
  mutationFn: () => DefaultService.confirmTotpEnrollment({ code: totpCode.value.trim() }),
  onSuccess: () => {
    clearError(); pendingEnrollment.value = null; totpCode.value = ''; totpOpen.value = false
    toast.success('两步验证已启用')
    void totp.refetch()
  },
  onError: (cause) => {
    if (toApiError(cause).code === apiCodes.totpEnrollmentChanged) {
      pendingEnrollment.value = null
      totpCode.value = ''
      showError(cause, '两步验证设置已更新，请重新开始启用。')
      return
    }
    showError(cause, totpMessage(cause, '验证码错误。'))
  },
})
const disableTotp = useMutation({
  mutationFn: () => DefaultService.disableTotp({ code: totpCode.value.trim() }),
  onSuccess: () => {
    clearError(); totpCode.value = ''; pendingEnrollment.value = null; totpOpen.value = false
    toast.success('两步验证已关闭')
    void totp.refetch()
  },
  onError: (cause) => { showError(cause, totpMessage(cause, '验证码错误。')) },
})
function totpMessage(cause: unknown, fallback: string) {
  const adapted = toApiError(cause)
  if (adapted.status === 429) return '尝试次数过多，请稍后再试。'
  if (adapted.code === apiCodes.totpInvalid) return fallback
  return displayError(cause)
}
const sessions = useQuery({ queryKey: queryKeys.sessions.all(), queryFn: () => DefaultService.listSessions() })
const devices = useQuery({ queryKey: queryKeys.sessions.devices(), queryFn: () => DefaultService.listDevices() })
const rename = useMutation({
  mutationFn: () => DefaultService.renameDevice(auth.device!.id, { name: name.value.trim() }),
  onSuccess: (device) => {
    clearError(); auth.device = device; renaming.value = false
    toast.success('设备名称已更新')
    void client.invalidateQueries({ queryKey: queryKeys.sessions.devices() })
  },
  onError: (cause) => { showError(cause) },
})
const revoke = useMutation({
  mutationFn: (sessionId: string) => DefaultService.revokeSession(sessionId),
  onSuccess: (_, sessionId) => {
    clearError()
    if (sessionId === auth.session?.id) auth.clear('guest')
    else {
      toast.success('会话已撤销')
      void client.invalidateQueries({ queryKey: queryKeys.sessions.all() })
    }
  },
  onError: (cause) => { showError(cause) },
})
const password = useMutation({
  mutationFn: () => DefaultService.changePassword({ currentPassword: currentPassword.value, newPassword: newPassword.value }),
  onSuccess: () => {
    clearError(); currentPassword.value = ''; newPassword.value = ''; passwordOpen.value = false
    toast.success('密码已修改，其他会话已撤销')
    void client.invalidateQueries({ queryKey: queryKeys.sessions.all() })
  },
  onError: (cause) => { showError(cause) },
})
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') emit('close') }
onMounted(async () => {
  document.addEventListener('keydown', onKey)
  returnFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
  await nextTick()
  drawer.value?.focus()
})
onUnmounted(() => {
  document.removeEventListener('keydown', onKey)
  returnFocusTo?.focus()
})
</script>

<template>
  <Teleport to="body">
    <div
      class="backdrop"
      @click.self="emit('close')"
    >
      <section
        ref="drawer"
        class="drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="sessions-title"
        tabindex="-1"
      >
        <header class="drawer-header">
          <span
            class="account-avatar"
            aria-hidden="true"
          >{{ avatarText }}</span>
          <div class="account-copy">
            <p class="eyebrow">
              账号安全
            </p>
            <h2 id="sessions-title">
              设备与会话
            </h2>
            <p class="account-meta">
              <strong>{{ displayName }}</strong><span>{{ username }} · {{ auth.device?.name || '当前设备' }}</span>
            </p>
          </div>
          <button
            class="icon-button"
            type="button"
            aria-label="关闭"
            @click="emit('close')"
          >
            <X aria-hidden="true" />
          </button>
        </header>

        <div class="drawer-content">
          <section
            class="security-card current-device"
            aria-labelledby="current-device-title"
          >
            <div class="card-heading">
              <span class="card-icon"><Laptop aria-hidden="true" /></span>
              <div>
                <h3 id="current-device-title">
                  当前设备
                </h3><p>此设备正在使用当前登录会话</p>
              </div>
              <span class="status-badge success"><CheckCircle2 aria-hidden="true" />当前</span>
            </div>
            <div
              v-if="!renaming"
              class="device-summary"
            >
              <div><strong>{{ auth.device?.name || '未命名设备' }}</strong><small>最近活动 · 现在</small></div>
              <button
                class="quiet-button"
                type="button"
                @click="renaming = true"
              >
                <Pencil aria-hidden="true" />重命名
              </button>
            </div>
            <form
              v-else
              class="inline-form"
              @submit.prevent="rename.mutate()"
            >
              <label class="field"><span>设备名称</span><input
                v-model="name"
                maxlength="100"
                required
                autofocus
              ></label>
              <div class="form-actions">
                <button
                  class="button"
                  type="button"
                  @click="renaming = false; name = auth.device?.name ?? ''"
                >
                  取消
                </button>
                <button
                  class="button primary"
                  :disabled="rename.isPending.value"
                >
                  保存
                </button>
              </div>
            </form>
          </section>

          <section
            class="security-card"
            aria-labelledby="sessions-list-title"
          >
            <div class="card-heading">
              <span class="card-icon"><Monitor aria-hidden="true" /></span>
              <div>
                <h3 id="sessions-list-title">
                  登录会话
                </h3><p>查看并管理已登录 RelayShelf 的设备</p>
              </div>
            </div>
            <p
              v-if="sessions.isPending.value"
              class="empty-state"
            >
              正在读取会话…
            </p>
            <p
              v-else-if="!sessions.data.value?.length"
              class="empty-state"
            >
              暂无登录会话
            </p>
            <ul
              v-else
              class="session-list"
            >
              <li
                v-for="session in sessions.data.value"
                :key="session.id"
                class="session-item"
                :class="{ current: session.current }"
              >
                <span class="session-icon"><component
                  :is="sessionIcon(session.deviceId)"
                  aria-hidden="true"
                /></span>
                <span class="session-copy">
                  <span class="session-title"><strong>{{ devices.data.value?.find((item) => item.id === session.deviceId)?.name || '未知设备' }}</strong><span
                    v-if="session.current"
                    class="status-badge success"
                  >当前会话</span></span>
                  <small>最近使用 {{ formatLastSeen(session.lastSeenAt) }}</small>
                </span>
                <button
                  v-if="!session.current"
                  class="revoke-button"
                  type="button"
                  :disabled="revoke.isPending.value"
                  @click="revoke.mutate(session.id)"
                >
                  撤销
                </button>
              </li>
            </ul>
          </section>

          <section
            class="security-card collapsible"
            aria-labelledby="password-title"
          >
            <button
              class="card-toggle"
              type="button"
              :aria-expanded="passwordOpen"
              aria-controls="password-form"
              @click="passwordOpen = !passwordOpen"
            >
              <span class="card-icon"><KeyRound aria-hidden="true" /></span>
              <span><strong id="password-title">登录密码</strong><small>更新密码并保护其他登录设备</small></span>
              <span class="status-badge neutral">安全设置</span>
              <ChevronDown
                class="chevron"
                :class="{ open: passwordOpen }"
                aria-hidden="true"
              />
            </button>
            <form
              v-if="passwordOpen"
              id="password-form"
              class="expandable-form"
              @submit.prevent="password.mutate()"
            >
              <p class="form-note">
                修改后将撤销除当前会话外的所有登录会话。
              </p>
              <label class="field"><span>当前密码</span><input
                v-model="currentPassword"
                type="password"
                autocomplete="current-password"
                required
              ></label>
              <label class="field"><span>新密码</span><input
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                minlength="10"
                required
              ></label>
              <div class="form-actions">
                <button
                  class="button primary"
                  :disabled="password.isPending.value"
                >
                  修改并撤销其他会话
                </button>
              </div>
            </form>
          </section>

          <section
            class="security-card collapsible"
            aria-labelledby="totp-title"
          >
            <button
              class="card-toggle"
              type="button"
              :aria-expanded="totpOpen"
              aria-controls="totp-form"
              @click="totpOpen = !totpOpen"
            >
              <span class="card-icon"><ShieldCheck aria-hidden="true" /></span>
              <span><strong id="totp-title">两步验证</strong><small>{{ totpEnabled ? '账号登录已受验证器保护' : '增加第二重登录保护' }}</small></span>
              <span
                class="status-badge"
                :class="totpEnabled ? 'success' : 'recommended'"
              >{{ totpEnabled ? '已启用' : '建议启用' }}</span>
              <ChevronDown
                class="chevron"
                :class="{ open: totpOpen }"
                aria-hidden="true"
              />
            </button>
            <div
              v-if="totpOpen"
              id="totp-form"
              class="expandable-form"
            >
              <template v-if="pendingEnrollment">
                <p class="form-note">
                  使用验证器应用扫描二维码，然后输入当前 6 位代码完成启用。
                </p>
                <TotpEnrollmentQr :uri="pendingEnrollment.otpauthUrl" />
                <p class="form-note">
                  无法扫码时，可手动输入以下密钥：
                </p>
                <p class="secret">
                  {{ pendingEnrollment.secret }}
                </p>
                <label class="field"><span>验证码</span><input
                  v-model="totpCode"
                  inputmode="numeric"
                  pattern="[0-9]{6}"
                  maxlength="6"
                  required
                ></label>
                <div class="form-actions">
                  <button
                    class="button primary"
                    type="button"
                    :disabled="confirmEnrollment.isPending.value"
                    @click="confirmEnrollment.mutate()"
                  >
                    确认并启用
                  </button>
                </div>
              </template>
              <template v-else-if="totpEnabled">
                <p class="form-note">
                  输入验证器中的当前 6 位代码以关闭两步验证。
                </p>
                <label class="field"><span>验证码</span><input
                  v-model="totpCode"
                  inputmode="numeric"
                  pattern="[0-9]{6}"
                  maxlength="6"
                  required
                ></label>
                <div class="form-actions">
                  <button
                    class="button danger"
                    type="button"
                    :disabled="disableTotp.isPending.value"
                    @click="disableTotp.mutate()"
                  >
                    关闭两步验证
                  </button>
                </div>
              </template>
              <template v-else>
                <p class="form-note">
                  为账号增加第二重登录保护；管理员在公开暴露前必须启用。
                </p>
                <label class="field"><span>当前密码</span><input
                  v-model="enrollmentPassword"
                  type="password"
                  autocomplete="current-password"
                  maxlength="1024"
                  required
                ></label>
                <div class="form-actions">
                  <button
                    class="button primary"
                    type="button"
                    :disabled="enroll.isPending.value || !enrollmentPassword"
                    @click="enroll.mutate()"
                  >
                    开始启用
                  </button>
                </div>
              </template>
            </div>
          </section>

          <p
            v-if="error"
            class="error-message"
            role="alert"
          >
            {{ error }}
          </p>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.backdrop{position:fixed;inset:0;z-index:50;display:flex;justify-content:flex-end;background:var(--surface-overlay)}
.drawer{width:min(620px,100vw);height:100%;min-width:0;overflow:auto;background:var(--surface-base);box-shadow:var(--shadow-floating);outline:0}
.drawer-header{position:sticky;top:0;z-index:2;display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.8rem;padding:1.15rem 1.25rem;border-bottom:1px solid var(--border-default);background:color-mix(in srgb,var(--surface-raised) 94%,transparent);backdrop-filter:blur(16px)}
.account-avatar{display:grid;place-items:center;width:48px;height:48px;border-radius:14px;background:var(--accent-primary-soft);color:var(--accent-primary-hover);font-size:1.05rem;font-weight:800}.account-copy{min-width:0}.eyebrow{margin:0 0 .2rem;color:var(--accent-primary);font:700 .62rem/1 var(--font-mono);letter-spacing:.11em;text-transform:uppercase}h2{margin:0;font-size:1.2rem;letter-spacing:-.02em}.account-meta{display:flex;min-width:0;gap:.35rem;margin:.28rem 0 0;color:var(--text-tertiary);font-size:.72rem}.account-meta strong,.account-meta span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.account-meta strong{flex:none;color:var(--text-secondary)}
.icon-button{display:grid;place-items:center;width:40px;height:40px;padding:0;border:0;border-radius:10px;background:transparent;color:var(--text-secondary)}.icon-button:hover{background:var(--surface-soft);color:var(--text-primary)}.icon-button svg{width:1.1rem;height:1.1rem}
.drawer-content{display:grid;gap:.9rem;padding:1rem 1.25rem 2rem}.security-card{min-width:0;padding:1rem;border:1px solid var(--border-default);border-radius:var(--radius);background:var(--surface-raised);box-shadow:var(--shadow-sm)}.card-heading{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.7rem}.card-heading h3{margin:0;font-size:.9rem}.card-heading p{margin:.2rem 0 0;color:var(--text-tertiary);font-size:.72rem;line-height:1.4}.card-icon,.session-icon{display:grid;place-items:center;flex:0 0 auto;width:38px;height:38px;border-radius:11px;background:var(--accent-primary-soft);color:var(--accent-primary)}.card-icon svg,.session-icon svg{width:1.05rem;height:1.05rem}
.status-badge{display:inline-flex;align-items:center;gap:.25rem;min-height:23px;padding:.18rem .48rem;border-radius:999px;font-size:.64rem;font-weight:750;white-space:nowrap}.status-badge svg{width:.72rem;height:.72rem}.status-badge.success{background:color-mix(in srgb,var(--state-success) 12%,var(--surface-soft));color:var(--state-success)}.status-badge.recommended{background:color-mix(in srgb,var(--state-warning) 13%,var(--surface-soft));color:var(--state-warning)}.status-badge.neutral{background:var(--surface-soft);color:var(--text-secondary)}
.device-summary{display:flex;align-items:center;justify-content:space-between;gap:.75rem;margin-top:.85rem;padding:.75rem;border-radius:var(--radius-sm);background:var(--surface-soft)}.device-summary strong,.device-summary small{display:block}.device-summary small{margin-top:.18rem;color:var(--text-tertiary);font-size:.68rem}.quiet-button,.revoke-button{display:inline-flex;align-items:center;justify-content:center;gap:.32rem;min-height:34px;padding:.35rem .55rem;border:0;border-radius:var(--radius-sm);background:transparent;color:var(--text-secondary);font-size:.72rem;white-space:nowrap}.quiet-button:hover{background:var(--surface-raised);color:var(--accent-primary)}.quiet-button svg{width:.8rem;height:.8rem}.revoke-button{color:var(--state-danger)}.revoke-button:hover{background:color-mix(in srgb,var(--state-danger) 9%,transparent)}.revoke-button:disabled{opacity:.5}
.inline-form,.expandable-form{display:grid;gap:.75rem;margin-top:.85rem;padding-top:.85rem;border-top:1px solid var(--border-default)}.field span{color:var(--text-secondary);font-size:.72rem;font-weight:650}.form-actions{display:flex;justify-content:flex-end;gap:.45rem;min-width:0}.form-note{margin:0;color:var(--text-tertiary);font-size:.75rem;line-height:1.55}.secret{min-width:0;margin:0;padding:.65rem;border-radius:var(--radius-sm);background:var(--surface-soft);font-family:var(--font-mono);font-size:.75rem;overflow-wrap:anywhere}
.session-list{display:grid;gap:.5rem;margin:.85rem 0 0;padding:0;list-style:none}.session-item{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.7rem;min-width:0;padding:.7rem;border:1px solid transparent;border-radius:var(--radius-sm);background:var(--surface-soft)}.session-item.current{border-color:color-mix(in srgb,var(--accent-primary) 28%,var(--border-default));background:color-mix(in srgb,var(--accent-primary-soft) 48%,var(--surface-raised))}.session-copy{display:grid;min-width:0}.session-title{display:flex;align-items:center;gap:.4rem;min-width:0}.session-title strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.8rem}.session-copy small{margin-top:.2rem;color:var(--text-tertiary);font-size:.68rem}.empty-state{margin:.85rem 0 0;padding:.8rem;border-radius:var(--radius-sm);background:var(--surface-soft);color:var(--text-tertiary);font-size:.75rem;text-align:center}
.collapsible{padding:0;overflow:hidden}.card-toggle{display:grid;grid-template-columns:auto minmax(0,1fr) auto auto;align-items:center;gap:.7rem;width:100%;padding:1rem;border:0;background:transparent;text-align:left}.card-toggle>span:nth-child(2){display:grid;min-width:0}.card-toggle strong{font-size:.9rem}.card-toggle small{margin-top:.2rem;color:var(--text-tertiary);font-size:.72rem;line-height:1.4}.card-toggle:hover{background:var(--surface-soft)}.chevron{width:1rem;height:1rem;color:var(--text-tertiary);transition:transform .16s ease}.chevron.open{transform:rotate(180deg)}.collapsible .expandable-form{margin:0;padding:0 1rem 1rem}.error-message{margin:0;padding:.7rem .8rem;border:1px solid color-mix(in srgb,var(--state-danger) 35%,var(--border-default));border-radius:var(--radius-sm);background:color-mix(in srgb,var(--state-danger) 8%,var(--surface-raised));color:var(--state-danger);font-size:.75rem}
@media(prefers-reduced-motion:no-preference){.drawer{animation:enter .16s ease-out}@keyframes enter{from{transform:translateX(20px);opacity:.7}}}
@media(max-width:600px){.drawer{width:100vw}.drawer-header{padding:.85rem}.account-avatar{width:42px;height:42px;border-radius:12px}.account-meta{display:grid;gap:.12rem}.drawer-content{padding:.75rem .7rem calc(1.5rem + env(safe-area-inset-bottom))}.security-card{padding:.85rem}.card-heading{grid-template-columns:auto minmax(0,1fr)}.card-heading>.status-badge{grid-column:2;justify-self:start}.device-summary{align-items:flex-start}.session-item{grid-template-columns:auto minmax(0,1fr)}.session-item>.revoke-button{grid-column:2;justify-self:start}.session-title{align-items:flex-start;flex-direction:column}.card-toggle{grid-template-columns:auto minmax(0,1fr) auto;padding:.85rem}.card-toggle>.status-badge{grid-column:2;justify-self:start}.card-toggle>.chevron{grid-column:3;grid-row:1/3}.collapsible .expandable-form{padding:0 .85rem .85rem}.form-actions{flex-wrap:wrap}.form-actions .button{max-width:100%;white-space:normal}}
@media(max-width:350px){.drawer-header{grid-template-columns:minmax(0,1fr) auto}.account-avatar{display:none}.device-summary{display:grid}.quiet-button{justify-self:start}.form-actions{display:grid}.form-actions .button{width:100%}}
</style>
