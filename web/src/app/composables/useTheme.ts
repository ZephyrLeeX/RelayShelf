import { computed, onScopeDispose, ref, watch } from 'vue'
import { THEME_STORAGE_KEY, useUiStore, type ThemeMode } from '@/app/stores/ui'

export function readThemeMode(): ThemeMode {
  if (typeof localStorage === 'undefined') return 'system'
  const stored = localStorage.getItem(THEME_STORAGE_KEY)
  return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system'
}

function systemTheme(): Exclude<ThemeMode, 'system'> {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function applyTheme(mode: ThemeMode) {
  if (typeof document === 'undefined') return
  document.documentElement.dataset.theme = mode === 'system' ? systemTheme() : mode
  document.documentElement.dataset.themeMode = mode
}

let stopInitialSystemListener: (() => void) | undefined

export function initializeTheme() {
  applyTheme(readThemeMode())
  if (typeof window === 'undefined' || !window.matchMedia) return
  stopInitialSystemListener?.()
  const media = window.matchMedia('(prefers-color-scheme: dark)')
  const listener = () => { if (readThemeMode() === 'system') applyTheme('system') }
  media.addEventListener('change', listener)
  stopInitialSystemListener = () => media.removeEventListener('change', listener)
}

export function useTheme() {
  const ui = useUiStore()
  const media = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : undefined
  const systemDark = ref(media?.matches ?? false)
  const resolvedTheme = computed(() => ui.themeMode === 'system' ? (systemDark.value ? 'dark' : 'light') : ui.themeMode)
  const stop = watch(() => ui.themeMode, (mode) => {
    localStorage.setItem(THEME_STORAGE_KEY, mode)
    applyTheme(mode)
  }, { immediate: true })
  const onSystemChange = (event: MediaQueryListEvent) => {
    systemDark.value = event.matches
    if (ui.themeMode === 'system') applyTheme('system')
  }
  media?.addEventListener('change', onSystemChange)
  onScopeDispose(() => {
    stop()
    media?.removeEventListener('change', onSystemChange)
  })
  return {
    themeMode: computed(() => ui.themeMode),
    resolvedTheme,
    setThemeMode: (mode: ThemeMode) => { ui.themeMode = mode },
  }
}
