import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ComposerEditor from './ComposerEditor.vue'

describe('shared rich text editor', () => {
  it.each([['加粗', '**文字**'], ['斜体', '*文字*'], ['列表', '\n- 文字\n'], ['链接', '[文字](https://example.com)'], ['引用', '\n> 文字\n'], ['代码块', '\n```\n文字\n```\n']])('inserts %s around the selection', async (label, result) => {
    const wrapper = mount(ComposerEditor, { props: { code: false, modelValue: '文字' } })
    wrapper.get('textarea').element.setSelectionRange(0, 2)
    await wrapper.findAll('button').find(button => button.text() === label)!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([result])
    expect(wrapper.emitted('format')).toHaveLength(1)
    wrapper.unmount()
  })
  it('suppresses IME shortcuts including keyCode 229 and composition events', async () => {
    const wrapper = mount(ComposerEditor, { props: { code: false, modelValue: '' } })
    const input = wrapper.get('textarea')
    await input.trigger('compositionstart')
    await input.trigger('keydown', { key: 'Enter', ctrlKey: true })
    await input.trigger('compositionend')
    await input.trigger('keydown', { key: 'Enter', ctrlKey: true, isComposing: true })
    await input.trigger('keydown', { key: 'Enter', ctrlKey: true, keyCode: 229 })
    expect(wrapper.emitted('keydown')).toBeUndefined()
    await input.trigger('keydown', { key: 'Enter', ctrlKey: true })
    expect(wrapper.emitted('keydown')).toHaveLength(1)
    wrapper.unmount()
  })
})
