import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UploadQueue from './UploadQueue.vue'

describe('UploadQueue', () => {
  it('takes focus and closes with Escape', async () => {
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()
    const wrapper = mount(UploadQueue, {
      attachTo: document.body,
      global: {
        stubs: {
          teleport: true,
          UploadItem: true,
          ResumeUploads: true,
        },
      },
    })
    await wrapper.vm.$nextTick()
    expect(document.activeElement).toBe(wrapper.get('.queue').element)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
    expect(document.activeElement).toBe(opener)
  })
})
