import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import { useDetailSelection } from './useDetailSelection'

const Harness = defineComponent({
  setup() {
    return useDetailSelection()
  },
  template: '<button class="open" @click="openDetail(\'message-1\')">{{ selectedMessageId }}</button><button class="close" @click="closeDetail">close</button>',
})

async function render(url = '/temporary?q=nginx&tagId=tag-1') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/temporary', component: Harness }],
  })
  await router.push(url)
  await router.isReady()
  return { router, wrapper: mount(Harness, { global: { plugins: [router] } }) }
}

describe('useDetailSelection', () => {
  it('opens and closes a detail without dropping unrelated query filters', async () => {
    const { router, wrapper } = await render()
    await wrapper.get('.open').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ q: 'nginx', tagId: 'tag-1', detail: 'message-1' })

    await router.replace({ query: { ...router.currentRoute.value.query, attachment: 'file-1' } })
    await wrapper.get('.close').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ q: 'nginx', tagId: 'tag-1' })
  })

  it('lets browser back and forward control the selected detail', async () => {
    const { router, wrapper } = await render('/temporary?q=nginx')
    await wrapper.get('.open').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.detail).toBe('message-1')

    router.back()
    await flushPromises()
    expect(router.currentRoute.value.query.detail).toBeUndefined()

    router.forward()
    await flushPromises()
    expect(router.currentRoute.value.query.detail).toBe('message-1')
  })
})
