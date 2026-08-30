import { defineStore } from 'pinia'

export type ThemeMode = 'system' | 'light' | 'dark'
export const THEME_STORAGE_KEY = 'relayshelf.theme'

function initialThemeMode(): ThemeMode {
  if (typeof localStorage === 'undefined') return 'system'
  const stored = localStorage.getItem(THEME_STORAGE_KEY)
  return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system'
}

export const useUiStore = defineStore('ui', {
  state: () => ({
    themeMode: initialThemeMode(),
    uploadQueueOpen: false,
    sessionsOpen: false,
    mobileMoreOpen: false,
  }),
})
