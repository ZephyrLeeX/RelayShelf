import { createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import AppSidebar from './AppSidebar.vue'

vi.mock('@/features/tags/queries', () => ({
  useTagsQuery: () => ({ data: ref([]), isPending: ref(false) }),
}))

const RouterLink = { props: ['to'], template: '<a :href="to"><slot /></a>' }

describe('AppSidebar', () => {
  it('owns desktop primary navigation and shell action entry points', async () => {
    const wrapper = mount(AppSidebar, {
      props: { uploadCount: 2, activeTransfers: true, deviceCount: 3, realtimeState: 'connected' },
      global: { plugins: [createPinia()], stubs: { RouterLink } },
    })
    const destinations = wrapper.findAll('a').map((link) => link.attributes('href'))
    expect(destinations).toEqual(expect.arrayContaining(['/temporary', '/permanent', '/search', '/favorites', '/trash']))
    expect(wrapper.text()).toContain('实时连接正常')
    expect(wrapper.text()).toContain('3 个设备')
    expect(wrapper.text()).not.toContain('在线')
    await wrapper.get('.status-card .queue').trigger('click')
    await wrapper.get('.account-button').trigger('click')
    expect(wrapper.emitted('openUploads')).toHaveLength(1)
    expect(wrapper.emitted('openSessions')).toHaveLength(1)
  })
})
