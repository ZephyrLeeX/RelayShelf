import { VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import { createApp, type Component } from 'vue'
import { configureApi } from '@/shared/api/configure'
import { setAuthExpiredHandler } from '@/shared/api/authExpiry'
import { useAuthStore } from '@/features/auth/store'
import { queryClient } from './queryClient'
import { router } from './router'

export function bootstrapApplication(root: Component) {
  configureApi()
  const app = createApp(root)
  const pinia = createPinia()
  app.use(pinia)
  app.use(router)
  app.use(VueQueryPlugin, { queryClient })
  setAuthExpiredHandler(() => {
    const redirect = router.currentRoute.value.fullPath
    useAuthStore(pinia).clear('guest')
    if (router.currentRoute.value.name !== 'login') void router.replace({ name: 'login', query: { redirect } })
  })
  return app
}
