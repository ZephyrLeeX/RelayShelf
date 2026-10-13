<script setup lang="ts">
import { X, Monitor, Pencil, ShieldCheck, KeyRound } from '@lucide/vue'
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
const renaming = ref(false)
const passwordExpanded = ref(false)
const totpExpanded = ref(false)
const displayName = computed(() => auth.user?.displayName || auth.user?.username || '账号')
const currentPassword = ref('')
const newPassword = ref('')
const drawer = ref<HTMLElement>()
let returnFocusTo: HTMLElement | null = null
const enrollmentPassword = ref('')
const totpCode = ref('')
const pendingEnrollment = ref<TOTPEnrollmentPending | null>(null)
const totp = useQuery({ queryKey: ['auth', 'totp'], queryFn: () => DefaultService.getTotpStatus() })
const totpEnabled = computed(() => totp.data.value?.enabled === true)
const enroll = useMutation({
  mutationFn: () => DefaultService.startTotpEnrollment({ currentPassword: enrollmentPassword.value }),
  onSuccess: (data) => { enrollmentPassword.value = ''; pendingEnrollment.value = data; totpCode.value = '' },
  onError: (cause) => { enrollmentPassword.value = ''; toast.error(displayError(cause)) },
})
const confirmEnrollment = useMutation({
  mutationFn: () => DefaultService.confirmTotpEnrollment({ code: totpCode.value.trim() }),
  onSuccess: () => { pendingEnrollment.value = null; totpCode.value = ''; totpExpanded.value = false; toast.success('两步验证已启用'); void totp.refetch() },
  onError: (cause) => {
    if (toApiError(cause).code === apiCodes.totpEnrollmentChanged) {
      pendingEnrollment.value = null
      totpCode.value = ''
      toast.error('两步验证设置已更新，请重新开始启用。')
      return
    }
    toast.error(totpMessage(cause, '验证码错误。'))
  },
})
const disableTotp = useMutation({
  mutationFn: () => DefaultService.disableTotp({ code: totpCode.value.trim() }),
  onSuccess: () => { totpCode.value = ''; pendingEnrollment.value = null; totpExpanded.value = false; toast.success('两步验证已关闭'); void totp.refetch() },
  onError: (cause) => { toast.error(totpMessage(cause, '验证码错误。')) },
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
  onSuccess: (device) => { auth.device = device; renaming.value = false; toast.success('设备名已保存'); void client.invalidateQueries({ queryKey: queryKeys.sessions.devices() }) },
  onError: (cause) => { toast.error(displayError(cause)) },
})
const revoke = useMutation({
  mutationFn: (sessionId: string) => DefaultService.revokeSession(sessionId),
  onSuccess: (_, sessionId) => {
    toast.success('会话已撤销')
    if (sessionId === auth.session?.id) auth.clear('guest')
    else void client.invalidateQueries({ queryKey: queryKeys.sessions.all() })
  },
  onError: (cause) => { toast.error(displayError(cause)) },
})
const password = useMutation({
  mutationFn: () => DefaultService.changePassword({ currentPassword: currentPassword.value, newPassword: newPassword.value }),
  onSuccess: () => { currentPassword.value = ''; newPassword.value = ''; passwordExpanded.value = false; toast.success('密码已修改，其他会话已撤销'); void client.invalidateQueries({ queryKey: queryKeys.sessions.all() }) },
  onError: (cause) => { toast.error(displayError(cause)) },
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
          <div
            class="avatar"
            aria-hidden="true"
          >
            {{ displayName.slice(0, 1).toUpperCase() }}
          </div>
          <div class="account-copy">
            <p class="eyebrow">
              设备与会话
            </p>
            <h2 id="sessions-title">
              <span class="sr-only">设备与会话 · </span>{{ displayName }}
            </h2>
            <p class="muted">
              @{{ auth.user?.username }} · {{ auth.device?.name }}
            </p>
          </div>
          <button
            class="button icon-button"
            aria-label="关闭"
            @click="emit('close')"
          >
            <X aria-hidden="true" />
          </button>
        </header>
        <section class="section current-device">
          <div class="section-heading">
            <div class="section-title">
              <Monitor aria-hidden="true" /><h3>当前设备</h3>
            </div>
            <span class="badge success">正在使用</span>
          </div>
          <div class="device-summary">
            <strong>{{ auth.device?.name }}</strong>
            <button
              class="button quiet"
              :aria-expanded="renaming"
              aria-controls="rename-form"
              @click="renaming = !renaming"
            >
              <Pencil aria-hidden="true" />重命名
            </button>
          </div>
          <form
            v-if="renaming"
            id="rename-form"
            class="expanded-form"
            @submit.prevent="rename.mutate()"
          >
            <label class="field">当前设备名<input
              v-model="name"
              maxlength="100"
              required
            ></label><button
              class="button"
              :disabled="rename.isPending.value"
            >
              保存设备名
            </button>
          </form>
        </section>
        <section class="section">
          <div class="section-heading">
            <h3>登录会话</h3><span class="muted">{{ sessions.data.value?.length ?? 0 }} 个会话</span>
          </div><p class="muted">
            查看登录活动，撤销不再使用的会话。
          </p><p v-if="sessions.isPending.value">
            正在读取…
          </p><ul v-else>
            <li
              v-for="session in sessions.data.value"
              :key="session.id"
              class="session-card"
              :class="{ current: session.current }"
            >
              <Monitor
                class="session-icon"
                aria-hidden="true"
              /><span class="session-copy">{{ devices.data.value?.find((item) => item.id === session.deviceId)?.name || '未知设备' }} <strong
                v-if="session.current"
                class="badge"
              >当前会话</strong><small>最近使用 {{ new Date(session.lastSeenAt).toLocaleString() }}</small></span><button
                v-if="!session.current"
                class="button danger quiet"
                :disabled="revoke.isPending.value"
                @click="revoke.mutate(session.id)"
              >
                撤销
              </button>
            </li>
          </ul>
        </section>
        <section class="section">
          <div class="section-heading">
            <div class="section-title">
              <KeyRound aria-hidden="true" /><h3>登录密码</h3>
            </div>
          </div>
          <p class="muted">
            修改密码后，其他设备上的登录会话将被撤销。
          </p>
          <button
            class="button disclosure"
            :aria-expanded="passwordExpanded"
            aria-controls="password-form"
            @click="passwordExpanded = !passwordExpanded"
          >
            {{ passwordExpanded ? '收起密码表单' : '修改密码' }}
          </button>
          <form
            v-if="passwordExpanded"
            id="password-form"
            class="expanded-form"
            @submit.prevent="password.mutate()"
          >
            <label class="field">当前密码<input
              v-model="currentPassword"
              type="password"
              autocomplete="current-password"
              required
            ></label><label class="field">新密码<input
              v-model="newPassword"
              type="password"
              autocomplete="new-password"
              minlength="10"
              required
            ></label><button
              class="button"
              :disabled="password.isPending.value"
            >
              修改并撤销其他会话
            </button>
          </form>
        </section>
        <section class="section">
          <div class="section-heading">
            <div class="section-title">
              <ShieldCheck aria-hidden="true" /><h3>两步验证（TOTP）</h3>
            </div><span
              class="badge"
              :class="{ success: totpEnabled }"
            >{{ totp.isPending.value ? '正在读取' : totp.isError.value ? '状态读取失败' : totpEnabled ? '已启用' : '建议启用' }}</span>
          </div>
          <p class="muted">
            {{ totpEnabled ? '登录时使用验证器代码，为账号提供第二重保护。' : '为账号增加第二重登录保护；管理员在公开暴露前必须启用。' }}
          </p>
          <button
            class="button disclosure"
            :aria-expanded="totpExpanded"
            aria-controls="totp-form"
            @click="totpExpanded = !totpExpanded"
          >
            {{ totpExpanded ? '收起验证设置' : totpEnabled ? '管理两步验证' : '设置两步验证' }}
          </button>
          <div
            v-show="totpExpanded"
            id="totp-form"
            class="expanded-form"
          >
            <template v-if="pendingEnrollment">
              <p class="muted">
                使用验证器应用扫描二维码，然后输入当前 6 位代码完成启用。
              </p>
              <TotpEnrollmentQr :uri="pendingEnrollment.otpauthUrl" />
              <p class="muted">
                无法扫码时，可手动输入以下密钥：
              </p>
              <p class="secret">
                {{ pendingEnrollment.secret }}
              </p>
              <label class="field">验证码<input
                v-model="totpCode"
                inputmode="numeric"
                pattern="[0-9]{6}"
                maxlength="6"
                required
              ></label>
              <button
                class="button primary"
                :disabled="confirmEnrollment.isPending.value"
                @click="confirmEnrollment.mutate()"
              >
                确认并启用
              </button>
            </template>
            <template v-else-if="totpEnabled">
              <p class="muted">
                已启用。输入当前验证码可关闭两步验证。
              </p>
              <label class="field">验证码<input
                v-model="totpCode"
                inputmode="numeric"
                pattern="[0-9]{6}"
                maxlength="6"
                required
              ></label>
              <button
                class="button danger"
                :disabled="disableTotp.isPending.value"
                @click="disableTotp.mutate()"
              >
                关闭两步验证
              </button>
            </template>
            <template v-else>
              <label class="field">当前密码<input
                v-model="enrollmentPassword"
                type="password"
                autocomplete="current-password"
                maxlength="1024"
                required
              ></label>
              <button
                class="button"
                :disabled="enroll.isPending.value || !enrollmentPassword"
                @click="enroll.mutate()"
              >
                开始启用
              </button>
            </template>
          </div>
        </section>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.backdrop { position:fixed; inset:0; z-index:50; background:var(--surface-overlay); display:flex; justify-content:flex-end; }
.drawer { width:min(100vw, 624px); height:100%; overflow:auto; padding:var(--space-5); background:var(--surface-base); box-shadow:var(--shadow-floating); display:flex; flex-direction:column; gap:var(--space-4); }
.drawer-header { display:flex; align-items:center; gap:var(--space-3); padding:var(--space-2) 0 var(--space-3); }
.avatar { display:grid; place-items:center; flex:0 0 52px; height:52px; border-radius:var(--radius-lg); background:var(--accent-primary-soft); color:var(--accent-primary); font-size:1.35rem; font-weight:700; }
.account-copy { flex:1; min-width:0; overflow-wrap:anywhere; }.eyebrow { color:var(--text-secondary); font-size:.75rem; font-weight:600; }h2 { font-size:1.3rem; }h3 { font-size:.95rem; }h2,h3,p { margin:0; }p { line-height:1.6; }.muted,small { font-size:.8rem; }
.icon-button { flex-shrink:0; display:grid; place-items:center; padding:.6rem; }svg { width:1rem; height:1rem; flex-shrink:0; }
.section { flex-shrink:0; display:grid; gap:var(--space-3); padding:var(--space-4); border:1px solid var(--border-default); border-radius:var(--radius-lg); background:var(--surface-raised); box-shadow:var(--shadow-sm); }
.section-heading,.section-title,.device-summary { display:flex; align-items:center; gap:var(--space-2); }.section-heading,.device-summary { justify-content:space-between; flex-wrap:wrap; }.section-title svg { color:var(--text-secondary); }.device-summary strong { overflow-wrap:anywhere; min-width:0; flex:1; }
.badge { display:inline-flex; align-items:center; width:fit-content; border-radius:var(--radius-sm); padding:.2rem .45rem; background:var(--accent-primary-soft); color:var(--accent-primary); font-size:.7rem; font-weight:600; }.badge.success { background:color-mix(in srgb,var(--state-success) 12%,var(--surface-raised)); color:var(--state-success); }
.expanded-form { display:grid; gap:var(--space-3); min-width:0; padding-top:var(--space-2); }.field { font-size:.85rem; }.field input { min-width:0; }.button { max-width:100%; }.quiet { box-shadow:none; background:transparent; border-color:transparent; display:inline-flex; align-items:center; justify-content:center; gap:var(--space-2); font-size:.8rem; }.disclosure { justify-self:start; font-size:.8rem; }
ul { list-style:none; padding:0; margin:0; display:grid; gap:var(--space-2); }.session-card { display:flex; align-items:center; gap:var(--space-3); padding:var(--space-3); border:1px solid var(--border-default); border-radius:var(--radius); }.session-card.current { background:var(--surface-soft); }.session-icon { color:var(--text-secondary); }.session-copy { flex:1; min-width:0; overflow-wrap:anywhere; font-size:.85rem; }.session-copy .badge { margin:.25rem; }small { display:block; color:var(--text-secondary); margin-top:.35rem; line-height:1.5; }
.secret { font-family:var(--font-mono); overflow-wrap:anywhere; padding:var(--space-3); background:var(--surface-soft); border-radius:var(--radius-sm); }
@media(max-width:480px) { .drawer { padding:var(--space-3); }.section { padding:var(--space-3); }.session-card { gap:var(--space-2); } }
@media (prefers-reduced-motion:no-preference) { .drawer { animation:enter .16s ease-out; } @keyframes enter { from { transform:translateX(20px); opacity:.7; } } }
</style>
