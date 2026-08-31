<script setup lang="ts">
import { X } from '@lucide/vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { DefaultService, type TOTPEnrollmentPending } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { apiCodes, displayError, toApiError } from '@/shared/api/errors'
import { useAuthStore } from '@/features/auth/store'
import TotpEnrollmentQr from './TotpEnrollmentQr.vue'

const emit = defineEmits<{ close: [] }>()
const auth = useAuthStore()
const client = useQueryClient()
const name = ref(auth.device?.name ?? '')
const error = ref('')
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
  onError: (cause) => { enrollmentPassword.value = ''; error.value = displayError(cause) },
})
const confirmEnrollment = useMutation({
  mutationFn: () => DefaultService.confirmTotpEnrollment({ code: totpCode.value.trim() }),
  onSuccess: () => { pendingEnrollment.value = null; totpCode.value = ''; void totp.refetch() },
  onError: (cause) => {
    if (toApiError(cause).code === apiCodes.totpEnrollmentChanged) {
      pendingEnrollment.value = null
      totpCode.value = ''
      error.value = '两步验证设置已更新，请重新开始启用。'
      return
    }
    error.value = totpMessage(cause, '验证码错误。')
  },
})
const disableTotp = useMutation({
  mutationFn: () => DefaultService.disableTotp({ code: totpCode.value.trim() }),
  onSuccess: () => { totpCode.value = ''; pendingEnrollment.value = null; void totp.refetch() },
  onError: (cause) => { error.value = totpMessage(cause, '验证码错误。') },
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
  onSuccess: (device) => { auth.device = device; void client.invalidateQueries({ queryKey: queryKeys.sessions.devices() }) },
  onError: (cause) => { error.value = displayError(cause) },
})
const revoke = useMutation({
  mutationFn: (sessionId: string) => DefaultService.revokeSession(sessionId),
  onSuccess: (_, sessionId) => {
    if (sessionId === auth.session?.id) auth.clear('guest')
    else void client.invalidateQueries({ queryKey: queryKeys.sessions.all() })
  },
  onError: (cause) => { error.value = displayError(cause) },
})
const password = useMutation({
  mutationFn: () => DefaultService.changePassword({ currentPassword: currentPassword.value, newPassword: newPassword.value }),
  onSuccess: () => { currentPassword.value = ''; newPassword.value = ''; void client.invalidateQueries({ queryKey: queryKeys.sessions.all() }) },
  onError: (cause) => { error.value = displayError(cause) },
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
        class="drawer panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="sessions-title"
        tabindex="-1"
      >
        <header>
          <div>
            <h2 id="sessions-title">
              设备与会话
            </h2><p class="muted">
              {{ auth.user?.displayName || auth.user?.username }} · 管理当前设备和登录安全
            </p>
          </div><button
            class="button"
            aria-label="关闭"
            @click="emit('close')"
          >
            <X aria-hidden="true" />
          </button>
        </header>
        <form
          class="section"
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
        <div class="section">
          <h3>登录会话</h3><p v-if="sessions.isPending.value">
            正在读取…
          </p><ul v-else>
            <li
              v-for="session in sessions.data.value"
              :key="session.id"
            >
              <span>{{ devices.data.value?.find((item) => item.id === session.deviceId)?.name || '未知设备' }} <strong v-if="session.current">当前</strong><small>最近使用 {{ new Date(session.lastSeenAt).toLocaleString() }}</small></span><button
                v-if="!session.current"
                class="button danger"
                @click="revoke.mutate(session.id)"
              >
                撤销
              </button>
            </li>
          </ul>
        </div>
        <form
          class="section"
          @submit.prevent="password.mutate()"
        >
          <h3>修改密码</h3><label class="field">当前密码<input
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
        <div class="section">
          <h3>两步验证（TOTP）</h3>
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
            <p class="muted">
              为账号增加第二重登录保护；管理员在公开暴露前必须启用。
            </p>
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
        <p
          v-if="error"
          class="error"
          role="alert"
        >
          {{ error }}
        </p>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.backdrop { position:fixed; inset:0; z-index:50; background:rgb(0 0 0 / .45); display:flex; justify-content:flex-end; }
.drawer { width:min(100%, 520px); height:100%; overflow:auto; border-radius:0; padding:1.25rem; }
header,li { display:flex; justify-content:space-between; gap:1rem; align-items:center; } header button svg { width:1rem; height:1rem; } h2,h3,p { margin:.25rem 0; }
.section { display:grid; gap:.75rem; padding:1.25rem 0; border-top:1px solid var(--border); } .secret { font-family:var(--font-mono); word-break:break-all; padding:.5rem; background:var(--surface-soft); border-radius:var(--radius-sm); } ul { list-style:none; padding:0; display:grid; gap:.65rem; } small { display:block; color:var(--muted); margin-top:.2rem; }
@media (prefers-reduced-motion:no-preference) { .drawer { animation:enter .16s ease-out; } @keyframes enter { from { transform:translateX(20px); opacity:.7; } } }
</style>
