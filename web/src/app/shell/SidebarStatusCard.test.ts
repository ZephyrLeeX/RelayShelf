import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { HealthState, StorageThresholdState } from '@/api/generated'
import SidebarStatusCard from './SidebarStatusCard.vue'

describe('SidebarStatusCard', () => {
  it('reports factual device, realtime, storage, and upload state without claiming presence', async () => {
    const wrapper = mount(SidebarStatusCard, { props: {
      device: 'Work laptop', deviceCount: 3, realtimeState: 'connected', uploadCount: 2, active: false,
      storage: {
        state: HealthState.HEALTHY, logicalUsageBytes: 1024, maxStorageBytes: 4096,
        thresholdState: StorageThresholdState.NORMAL, nasAvailableBytes: 3072, nasTotalBytes: 4096,
        stagingUsageBytes: 0, stagingAvailableBytes: 4096, stagingTotalBytes: 4096, degradedReasons: [],
      },
    } })
    expect(wrapper.text()).toContain('实时连接正常')
    expect(wrapper.text()).toContain('3 个设备')
    expect(wrapper.text()).toContain('存储 1.0 KB / 4.0 KB')
    expect(wrapper.text()).not.toContain('在线')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('openUploads')).toHaveLength(1)
  })
})
