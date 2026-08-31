import { createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/features/auth/store'
import MobileMoreMenu from './MobileMoreMenu.vue'

vi.mock('@/features/tags/queries', () => ({
  useTagsQuery: () => ({ data: ref([{ id: 'tag-1', name: '工作', color: '#336699' }]), isPending: ref(false) }),
}))

const RouterLink = { props: ['to'], emits: ['click'], template: '<a :href="to" @click="$emit(\'click\')"><slot /></a>' }

describe('MobileMoreMenu', () => {
  it('contains the Task 5 secondary destinations and gates admin navigation', async () => {
    const pinia = createPinia()
    const auth = useAuthStore(pinia)
    auth.user = { id: 'user-1', username: 'alice', displayName: 'Alice', isAdmin: true }
    const wrapper = mount(MobileMoreMenu, {
      global: { plugins: [pinia], stubs: { RouterLink, teleport: true } },
    })
    expect(wrapper.text()).toContain('标签')
    expect(wrapper.text()).toContain('工作')
    expect(wrapper.text()).toContain('收藏')
    expect(wrapper.text()).toContain('回收站')
    expect(wrapper.text()).toContain('设备与会话')
    expect(wrapper.text()).toContain('管理')
    expect(wrapper.text()).toContain('退出登录')
    await wrapper.findAll('button').find((button) => button.text() === '设备与会话')!.trigger('click')
    expect(wrapper.emitted('openSessions')).toHaveLength(1)
    await wrapper.get('.logout').trigger('click')
    expect(wrapper.emitted('logout')).toHaveLength(1)
  })
})
