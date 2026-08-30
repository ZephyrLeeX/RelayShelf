import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MobileBottomNav from './MobileBottomNav.vue'

describe('MobileBottomNav', () => {
  it('exposes the five planned destinations without owning domain state', async () => {
    const wrapper = mount(MobileBottomNav, { global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })
    expect(wrapper.findAll('a')).toHaveLength(3)
    expect(wrapper.findAll('button')).toHaveLength(2)
    expect(wrapper.text()).toContain('临时')
    expect(wrapper.text()).toContain('长期')
    expect(wrapper.text()).toContain('搜索')
    expect(wrapper.text()).toContain('上传')
    expect(wrapper.text()).toContain('我的')
    await wrapper.findAll('button')[0].trigger('click')
    await wrapper.findAll('button')[1].trigger('click')
    expect(wrapper.emitted('openUploads')).toHaveLength(1)
    expect(wrapper.emitted('openMore')).toHaveLength(1)
  })
})
