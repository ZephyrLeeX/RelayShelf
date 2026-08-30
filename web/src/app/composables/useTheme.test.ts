import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ThemeToggle from '@/app/shell/ThemeToggle.vue'
import { applyTheme, initializeTheme } from './useTheme'
import { THEME_STORAGE_KEY } from '@/app/stores/ui'

class ThemeMedia {
  matches = false
  listeners = new Set<(event: MediaQueryListEvent) => void>()
  addEventListener(_type: string, listener: (event: MediaQueryListEvent) => void) { this.listeners.add(listener) }
  removeEventListener(_type: string, listener: (event: MediaQueryListEvent) => void) { this.listeners.delete(listener) }
  change(matches: boolean) {
    this.matches = matches
    for (const listener of this.listeners) listener({ matches } as MediaQueryListEvent)
  }
}

describe('theme foundation', () => {
  let media: ThemeMedia
  beforeEach(() => {
    media = new ThemeMedia()
    vi.stubGlobal('matchMedia', vi.fn(() => media))
    setActivePinia(createPinia())
    delete document.documentElement.dataset.theme
    delete document.documentElement.dataset.themeMode
  })

  it('applies the stored preference before mount', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'dark')
    initializeTheme()
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.dataset.themeMode).toBe('dark')
  })

  it('resolves system mode and responds to operating-system changes', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'system')
    initializeTheme()
    expect(document.documentElement.dataset.theme).toBe('light')
    media.change(true)
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('persists only a theme enum through the three-way toggle', async () => {
    applyTheme('light')
    const wrapper = mount(ThemeToggle)
    await wrapper.get('button:nth-of-type(3)').trigger('click')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(wrapper.get('button:nth-of-type(3)').attributes('aria-pressed')).toBe('true')
    wrapper.unmount()
  })
})
