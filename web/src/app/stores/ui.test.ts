import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { THEME_STORAGE_KEY, useUiStore } from './ui'

describe('UI store ownership', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('contains only shell UI state and restores a valid theme preference', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'dark')
    const ui = useUiStore()
    expect({ ...ui.$state }).toEqual({ themeMode: 'dark', uploadQueueOpen: false, sessionsOpen: false, mobileMoreOpen: false })
  })

  it('ignores an invalid persisted theme value', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'sensitive-body')
    expect(useUiStore().themeMode).toBe('system')
  })
})
