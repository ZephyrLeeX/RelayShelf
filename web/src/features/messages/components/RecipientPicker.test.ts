import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DefaultService, type RecipientUser } from '@/api/generated'
import RecipientPicker from './RecipientPicker.vue'

const bob: RecipientUser = { id: 'user-bob', username: 'bob', displayName: 'Bob Zhang' }
const carol: RecipientUser = { id: 'user-carol', username: 'carol', displayName: 'Carol' }

function mountPicker() {
  const host = defineComponent({
    components: { RecipientPicker },
    setup() {
      const recipient = ref<RecipientUser | null>(null)
      return { recipient }
    },
    template: '<RecipientPicker v-model="recipient" />',
  })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = mount(host, { global: { plugins: [[VueQueryPlugin, { queryClient: client }]] } })
  return { wrapper, host }
}

async function open(wrapper: ReturnType<typeof mountPicker>['wrapper']) {
  await wrapper.get('button[aria-label="接收人"]').trigger('click')
  await flushPromises()
}

describe('RecipientPicker', () => {
  beforeEach(() => {
    vi.spyOn(DefaultService, 'listRecipientUsers').mockImplementation((query?: string) => Promise.resolve({
      items: [bob, carol].filter((user) => !query
        || user.username.includes(query)
        || user.displayName.toLowerCase().includes(query.toLowerCase())),
    }) as never)
  })

  it('defaults to myself and never renders user IDs', async () => {
    const { wrapper } = mountPicker()
    expect(wrapper.get('button[aria-label="接收人"]').text()).toContain('自己')
    await open(wrapper)
    expect(wrapper.text()).not.toContain('user-bob')
    expect(wrapper.text()).not.toContain('user-carol')
  })

  it('searches by username and by display name', async () => {
    const { wrapper } = mountPicker()
    await open(wrapper)
    expect(wrapper.findAll('[role="option"]')).toHaveLength(3) // 自己 + bob + carol

    await wrapper.get('input[type="search"]').setValue('carol')
    await flushPromises()
    expect(DefaultService.listRecipientUsers).toHaveBeenLastCalledWith('carol', 8)
    expect(wrapper.findAll('[role="option"]')).toHaveLength(2) // 自己 + carol

    await wrapper.get('input[type="search"]').setValue('Bob Zhang')
    await flushPromises()
    expect(DefaultService.listRecipientUsers).toHaveBeenLastCalledWith('Bob Zhang', 8)
    expect(wrapper.text()).toContain('@bob')
  })

  it('selects a user, shows @username, and can restore myself', async () => {
    const { wrapper } = mountPicker()
    await open(wrapper)
    await wrapper.get('button[role="option"][aria-label="选择 Bob Zhang @bob"]').trigger('click')
    expect(wrapper.vm.recipient).toEqual(bob)
    expect(wrapper.get('button[aria-label="接收人"]').text()).toContain('@bob')

    await open(wrapper)
    const self = wrapper.findAll('[role="option"]').find((option) => option.text().includes('自己'))
    expect(self).toBeTruthy()
    await self!.trigger('click')
    expect(wrapper.vm.recipient).toBeNull()
    expect(wrapper.get('button[aria-label="接收人"]').text()).toContain('自己')
  })

  it('closes on Escape and outside pointer down', async () => {
    const { wrapper } = mountPicker()
    await open(wrapper)
    expect(wrapper.find('.recipient-menu').exists()).toBe(true)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('.recipient-menu').exists()).toBe(false)

    await open(wrapper)
    document.dispatchEvent(new Event('pointerdown'))
    await flushPromises()
    expect(wrapper.find('.recipient-menu').exists()).toBe(false)
  })
})
