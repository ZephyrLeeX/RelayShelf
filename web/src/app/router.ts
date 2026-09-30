import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import { pendingShare, receiveShare, shareNotice } from '@/features/share/receive'
import AppShell from './AppShell.vue'
import LoginLayout from './LoginLayout.vue'
import { useAuthStore } from '@/features/auth/store'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginLayout, children: [{ path: '', name: 'login', component: () => import('@/features/auth/LoginView.vue') }] },
    {
      path: '/', component: AppShell, meta: { private: true }, children: [
        { path: '', redirect: '/temporary' },
        { path: 'share', name: 'share', component: () => import('@/features/messages/views/FeedView.vue'), props: { kind: 'temporary' } },
        { path: 'temporary', name: 'temporary', component: () => import('@/features/messages/views/FeedView.vue'), props: { kind: 'temporary' } },
        { path: 'permanent', name: 'permanent', component: () => import('@/features/messages/views/FeedView.vue'), props: { kind: 'permanent' } },
        { path: 'favorites', name: 'favorites', component: () => import('@/features/messages/views/FeedView.vue'), props: { kind: 'favorites' } },
        { path: 'tags/:id', name: 'tag', component: () => import('@/features/messages/views/FeedView.vue'), props: (route) => ({ kind: 'tag', tagId: route.params.id }) },
        { path: 'trash', name: 'trash', component: () => import('@/features/messages/views/FeedView.vue'), props: { kind: 'trash' } },
        { path: 'search', name: 'search', component: () => import('@/features/search/SearchView.vue') },
        { path: 'messages/:id', name: 'message-detail', component: () => import('@/features/messages/views/MessageDetailView.vue'), props: true, meta: { shell: 'full' } },
        { path: 'admin/:pathMatch(.*)*', name: 'admin', component: () => import('@/features/admin/AdminView.vue'), meta: { admin: true, shell: 'full' } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/temporary' },
  ],
})

function loginRedirect(to: RouteLocationNormalized) {
  return { name: 'login', query: { redirect: to.fullPath } }
}

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.name === 'share' && to.hash) {
    await receiveShare(to.hash.slice(1))
    return { path: '/share', replace: true }
  }
  if (to.name === 'share' && !pendingShare.value && !shareNotice.value) {
    shareNotice.value = '没有待接收的分享。刷新或关闭页面会清除未发送内容，请重新分享或复制粘贴。'
  }
  if (to.name === 'login') {
    if (auth.status === 'unknown') await auth.bootstrap()
    return auth.status === 'authenticated' ? '/temporary' : true
  }
  if (!to.meta.private) return true
  if (auth.status === 'unknown' || auth.status === 'error') await auth.bootstrap()
  if (auth.status !== 'authenticated') return loginRedirect(to)
  if (to.meta.admin && !auth.user?.isAdmin) return '/temporary'
  return true
})
