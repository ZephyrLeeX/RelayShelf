<script setup lang="ts">
import { useTheme } from '@/app/composables/useTheme'
import type { ThemeMode } from '@/app/stores/ui'

const { themeMode, setThemeMode } = useTheme()
const modes: { value: ThemeMode, label: string }[] = [
  { value: 'system', label: '跟随系统' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
]
</script>

<template>
  <div
    class="theme-toggle"
    role="group"
    aria-label="主题模式"
  >
    <button
      v-for="mode in modes"
      :key="mode.value"
      type="button"
      :class="{ active: themeMode === mode.value }"
      :aria-pressed="themeMode === mode.value"
      @click="setThemeMode(mode.value)"
    >
      {{ mode.label }}
    </button>
  </div>
</template>

<style scoped>
.theme-toggle{display:flex;gap:.15rem;padding:.2rem;border:1px solid var(--border-default);border-radius:999px;background:var(--surface-soft)}
button{min-height:30px;border:0;border-radius:999px;padding:.3rem .55rem;background:transparent;color:var(--text-secondary);font-size:.72rem;white-space:nowrap}
button.active{background:var(--surface-raised);color:var(--text-primary);box-shadow:var(--shadow-sm);font-weight:700}
@media(max-width:520px){button{padding:.35rem .48rem}}
</style>
