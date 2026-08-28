<script setup lang="ts">
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { onMounted, onUnmounted, ref } from 'vue'
import { DefaultService } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { displayError } from '@/shared/api/errors'
import { useAuthStore } from '@/features/auth/store'

const emit = defineEmits<{ close: [] }>()
const auth = useAuthStore()
const client = useQueryClient()
const name = ref(auth.device?.name ?? '')
const error = ref('')
const currentPassword = ref('')
const newPassword = ref('')
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
onMounted(() => document.addEventListener('keydown', onKey))
onUnmounted(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <div
      class="backdrop"
      @click.self="emit('close')"
    >
      <section
        class="drawer panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="sessions-title"
      >
        <header>
          <div>
            <h2 id="sessions-title">
              设备与会话
            </h2><p class="muted">
              {{ auth.user?.displayName || auth.user?.username }}
            </p>
          </div><button
            class="button"
            aria-label="关闭"
            @click="emit('close')"
          >
            ×
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
header,li { display:flex; justify-content:space-between; gap:1rem; align-items:center; } h2,h3,p { margin:.25rem 0; }
.section { display:grid; gap:.75rem; padding:1.25rem 0; border-top:1px solid var(--border); } ul { list-style:none; padding:0; display:grid; gap:.65rem; } small { display:block; color:var(--muted); margin-top:.2rem; }
@media (prefers-reduced-motion:no-preference) { .drawer { animation:enter .16s ease-out; } @keyframes enter { from { transform:translateX(20px); opacity:.7; } } }
</style>
