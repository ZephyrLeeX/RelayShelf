import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SafeMarkdown from './SafeMarkdown.vue'

describe('SafeMarkdown', () => {
  it('blocks raw HTML, active protocols, handlers, and remote image loads', async () => {
    const wrapper = mount(SafeMarkdown, { props: { source: '<script>alert(1)</script>\n<img src=x onerror=alert(1)>\n[x](javascript:alert(1))\n![track](https://example.test/t.png)' } })
    await flushPromises()
    const html = wrapper.html()
    expect(html).not.toContain('<script')
    expect(html).not.toContain('<img')
    expect(wrapper.find('[onerror]').exists()).toBe(false)
    expect(html).not.toContain('href="javascript:')
    expect(html).toContain('远程图片链接')
  })

  it('allows http/https links with protected external navigation and highlights known languages', async () => {
    const wrapper = mount(SafeMarkdown, { props: { source: '[safe](https://example.test)\n```json\n{"ok":true}\n```' } })
    await vi.waitFor(() => expect(wrapper.find('a').exists()).toBe(true))
    const link = wrapper.get('a')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    expect(link.attributes('target')).toBe('_blank')
    expect(wrapper.html()).toContain('hljs')
  })

  it('falls back to escaped plain code for an unknown language', async () => {
    const wrapper = mount(SafeMarkdown, { props: { source: '```unknown-lang\n<script>\n```' } })
    await flushPromises()
    expect(wrapper.find('code').text()).toContain('<script>')
    expect(wrapper.find('code script').exists()).toBe(false)
  })

  it('renders code behind longer fences and keeps embedded ``` runs intact', async () => {
    const source = '````python\nprint("```")\n````'
    const wrapper = mount(SafeMarkdown, { props: { source } })
    await vi.waitFor(() => expect(wrapper.find('pre code').exists()).toBe(true))
    // The four-backtick fence keeps the inner ``` inside the code block
    // instead of closing it early (which would emit a second block).
    expect(wrapper.find('pre code').text()).toContain('print("```")')
    expect(wrapper.findAll('pre')).toHaveLength(1)
    expect(wrapper.find('p').exists()).toBe(false)
  })

  it('renders a heredoc containing a fenced json block without splitting it', async () => {
    const source = '`````bash\ncat <<\'EOF\'\n```json\n{"a":1}\n```\nEOF\n`````'
    const wrapper = mount(SafeMarkdown, { props: { source } })
    await vi.waitFor(() => expect(wrapper.find('pre code').exists()).toBe(true))
    expect(wrapper.findAll('pre')).toHaveLength(1)
    expect(wrapper.find('pre code').text()).toContain('cat <<\'EOF\'')
    expect(wrapper.find('pre code').text()).toContain('{"a":1}')
    wrapper.unmount()
  })

  it('blocks data: and other dangerous link schemes', async () => {
    const wrapper = mount(SafeMarkdown, { props: { source: '[a](data:text/html;base64,PHNjcmlwdD4)\n[b](vbscript:alert)\n[c](file:///etc/passwd)' } })
    await flushPromises()
    const html = wrapper.html()
    expect(html).not.toContain('href="data:')
    expect(html).not.toContain('href="vbscript:')
    expect(html).not.toContain('href="file:')
    wrapper.unmount()
  })
})
