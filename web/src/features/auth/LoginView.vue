<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { apiCodes, displayError, toApiError } from '@/shared/api/errors'
import { useAuthStore } from './store'

const username = ref('')
const password = ref('')
const totpCode = ref('')
const pendingChallenge = ref<string | null>(null)
const submitting = ref(false)
const error = ref('')
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

async function finishLogin() {
  const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/') ? route.query.redirect : '/temporary'
  await router.replace(redirect)
}

async function submit() {
  error.value = ''
  submitting.value = true
  try {
    if (pendingChallenge.value) {
      await auth.completeTotpLogin(pendingChallenge.value, totpCode.value.trim())
      pendingChallenge.value = null
      await finishLogin()
      return
    }
    const challenge = await auth.login(username.value, password.value)
    if (challenge) {
      pendingChallenge.value = challenge.challengeToken
      totpCode.value = ''
      return
    }
    await finishLogin()
  } catch (cause) {
    const adapted = toApiError(cause)
    if (adapted.code === apiCodes.totpChallengeExpired) {
      pendingChallenge.value = null
      password.value = ''
      error.value = '两步验证已过期，请重新登录。'
    } else if (adapted.code === apiCodes.totpInvalid || (adapted.status === 401 && pendingChallenge.value)) {
      error.value = '验证码错误。'
    } else if (adapted.code === apiCodes.invalidCredentials) {
      error.value = '用户名或密码错误。'
    } else if (adapted.status === 429) {
      error.value = '尝试次数过多，请稍后再试。'
    } else {
      error.value = displayError(cause)
    }
  } finally { submitting.value = false }
}
</script>

<template>
  <section
    class="login-card panel"
    aria-labelledby="login-heading"
  >
    <img
      src="/favicon.svg"
      alt=""
      width="48"
      height="48"
    >
    <div>
      <h1 id="login-heading">
        登录 RelayShelf
      </h1><p class="muted">
        在你的设备间取回刚刚放下的内容。
      </p>
    </div>
    <form @submit.prevent="submit">
      <template v-if="!pendingChallenge">
        <label class="field">用户名<input
          v-model="username"
          name="username"
          autocomplete="username"
          required
          autofocus
        ></label>
        <label class="field">密码<input
          v-model="password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
          minlength="10"
        ></label>
      </template>
      <template v-else>
        <p class="muted">
          此账号已启用两步验证。请输入验证器应用中的 6 位代码。
        </p>
        <label class="field">验证码<input
          v-model="totpCode"
          name="totp-code"
          inputmode="numeric"
          autocomplete="one-time-code"
          pattern="[0-9]{6}"
          maxlength="6"
          required
          autofocus
        ></label>
      </template>
      <p
        v-if="auth.status === 'error'"
        class="error"
      >
        启动检查失败：{{ auth.bootstrapError }}。你仍可尝试登录。
      </p>
      <p
        v-if="error"
        class="error"
        role="alert"
      >
        {{ error }}
      </p>
      <button
        class="button primary"
        type="submit"
        :disabled="submitting"
      >
        {{ submitting ? '登录中…' : pendingChallenge ? '验证' : '登录' }}
      </button>
    </form>
  </section>
</template>

<style scoped>
.login-card { width:min(100%, 420px); padding:2rem; display:grid; gap:1.5rem; }
h1,p { margin:.25rem 0; } form { display:grid; gap:1rem; }
</style>
