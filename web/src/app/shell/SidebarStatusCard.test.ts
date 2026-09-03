import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { StorageRuntimeStatus } from '@/api/generated'
import SidebarStatusCard from './SidebarStatusCard.vue'

describe('SidebarStatusCard', () => {
  it('reports factual device, realtime, storage, and upload state without claiming presence', async () => {
    const wrapper = mount(SidebarStatusCard, { props: {
      device: 'Work laptop', deviceCount: 3, realtimeState: 'connected', uploadCount: 2, active: false,
      storage: {
        healthy: true, reason: StorageRuntimeStatus.reason.HEALTHY, lastCheckedAt: '2026-09-03T00:00:00Z', changedAt: '2026-09-03T00:00:00Z',
      },
    } })
    expect(wrapper.text()).toContain('实时连接正常')
    expect(wrapper.text()).toContain('3 个设备')
    expect(wrapper.text()).toContain('存储 · 正常')
    expect(wrapper.text()).not.toContain('在线')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('openUploads')).toHaveLength(1)
  })

  it('shows degraded storage state', () => {
    const wrapper = mount(SidebarStatusCard, { props: {
      realtimeState: 'disconnected', uploadCount: 0, active: false,
      storage: { healthy: false, reason: StorageRuntimeStatus.reason.NAS_UNAVAILABLE, lastCheckedAt: null, changedAt: '2026-09-03T00:00:00Z' },
    } })
    expect(wrapper.text()).toContain('存储 · 不可用')
    expect(wrapper.get('.storage').attributes('data-state')).toBe('DEGRADED')
  })
})
