<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { apiCodes, displayError, toApiError } from '@/shared/api/errors'
import { useAuthStore } from './store'

const username = ref('')
const password = ref('')
const submitting = ref(false)
const error = ref('')
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

async function submit() {
  error.value = ''
  submitting.value = true
  try {
    await auth.login(username.value, password.value)
    const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/') ? route.query.redirect : '/temporary'
    await router.replace(redirect)
  } catch (cause) {
    const adapted = toApiError(cause)
    error.value = adapted.code === apiCodes.invalidCredentials
      ? '用户名或密码错误。'
      : adapted.status === 429 ? '尝试次数过多，请稍后再试。' : displayError(cause)
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
        {{ submitting ? '登录中…' : '登录' }}
      </button>
    </form>
  </section>
</template>

<style scoped>
.login-card { width:min(100%, 420px); padding:2rem; display:grid; gap:1.5rem; }
h1,p { margin:.25rem 0; } form { display:grid; gap:1rem; }
</style>
