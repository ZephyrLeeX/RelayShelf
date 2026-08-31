import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LinkifiedText from './LinkifiedText.vue'

function links(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('a').map((anchor) => ({ text: anchor.text(), href: anchor.attributes('href') }))
}

function render(text: string) {
  const wrapper = mount(LinkifiedText, { props: { text } })
  // Linkification may only turn URL text into anchors — never drop, add, or
  // rewrite body characters, so the rendered text must equal the input.
  expect(wrapper.element.textContent).toBe(text)
  return wrapper
}

describe('LinkifiedText', () => {
  it('linkifies https, http, and www URLs with normalized safe hrefs', () => {
    const wrapper = render('下载：https://example.com/file.zip 或 http://example.com 或 www.example.com/path')
    expect(links(wrapper)).toEqual([
      { text: 'https://example.com/file.zip', href: 'https://example.com/file.zip' },
      { text: 'http://example.com', href: 'http://example.com' },
      { text: 'www.example.com/path', href: 'https://www.example.com/path' },
    ])
    for (const anchor of wrapper.findAll('a')) {
      expect(anchor.attributes('target')).toBe('_blank')
      expect(anchor.attributes('rel')).toBe('noopener noreferrer')
    }
  })

  it('never creates links for dangerous schemes', () => {
    const text = 'javascript:alert(1) data:text/html,pwn file:///etc/passwd mailto:a@b.c ftp://x.test'
    const wrapper = render(text)
    expect(wrapper.findAll('a')).toHaveLength(0)
    expect(wrapper.text()).toContain('javascript:alert(1)')
    expect(wrapper.text()).toContain('file:///etc/passwd')
  })

  it.each([
    ',', '.', ';', ':', '!', '?', ')', ']', '}',
    '。', '，', '；', '：', '！', '？', '）', '】',
  ])('keeps trailing %s out of the href but in the body', (punctuation) => {
    const text = `见 https://example.com/a${punctuation}结束`
    const wrapper = render(text)
    expect(links(wrapper)).toEqual([{ text: 'https://example.com/a', href: 'https://example.com/a' }])
    expect(wrapper.element.textContent).toContain(`https://example.com/a${punctuation}结束`)
  })

  it('keeps every trimmed character when several punctuated URLs share a line', () => {
    const text = 'https://example.com/a。以及 https://example.com/b, https://example.com/c)；结束'
    const wrapper = render(text)
    expect(links(wrapper)).toEqual([
      { text: 'https://example.com/a', href: 'https://example.com/a' },
      { text: 'https://example.com/b', href: 'https://example.com/b' },
      { text: 'https://example.com/c', href: 'https://example.com/c' },
    ])
    expect(wrapper.element.textContent).toContain('；结束')
    expect(wrapper.element.textContent).toContain('b, https')
  })

  it('keeps balanced parentheses inside the URL', () => {
    const wrapper = render('wiki (https://example.com/A_(b)) end')
    expect(links(wrapper)).toEqual([{ text: 'https://example.com/A_(b)', href: 'https://example.com/A_(b)' }])
    expect(wrapper.element.textContent).toContain(') end')
  })

  it('preserves newlines and surrounding text as plain text nodes', () => {
    const wrapper = render('line one\nhttps://example.com\nline three')
    expect(wrapper.find('.linkified-text').element.children).toHaveLength(1)
  })

  it('does not match URLs embedded inside words', () => {
    const wrapper = render('awww.example.com and 1https://example.com')
    expect(wrapper.findAll('a')).toHaveLength(0)
  })

  it('renders a URL-only body without adding or dropping anything', () => {
    const wrapper = render('https://example.com/only')
    expect(links(wrapper)).toEqual([{ text: 'https://example.com/only', href: 'https://example.com/only' }])
  })
})
