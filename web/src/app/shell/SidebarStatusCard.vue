<script setup lang="ts">
import { ArrowUpRight } from '@lucide/vue'
import { computed } from 'vue'
import type { StorageRuntimeStatus } from '@/api/generated'
import type { RealtimeConnectionState } from '@/app/realtime'

const props = defineProps<{
  device?: string
  deviceCount?: number
  uploadCount: number
  active: boolean
  realtimeState: RealtimeConnectionState
  storage?: StorageRuntimeStatus
}>()
defineEmits<{ openUploads: [] }>()

const realtimeLabel = computed(() => ({
  idle: '实时连接未启动',
  connecting: '实时连接中',
  connected: '实时连接正常',
  disconnected: '实时连接已中断',
})[props.realtimeState])
const storageLabel = computed(() => {
  if (!props.storage) return ''
  return props.storage.healthy ? '存储 · 正常' : props.storage.reason === 'NAS_FULL' ? '存储 · 空间已满' : '存储 · 不可用'
})
</script>

<template>
  <section
    class="status-card"
    aria-label="连接与存储状态"
  >
    <span class="status-head"><i :class="{ connected: realtimeState === 'connected' }" /><strong>{{ realtimeLabel }}</strong></span>
    <span class="device">{{ device || '当前浏览器' }}<template v-if="deviceCount != null"> · {{ deviceCount }} 个设备</template></span>
    <span
      v-if="storageLabel"
      class="storage"
      :data-state="storage?.healthy ? 'HEALTHY' : 'DEGRADED'"
    >{{ storageLabel }}</span>
    <button
      type="button"
      class="queue"
      @click="$emit('openUploads')"
    >
      {{ active ? '正在中继' : uploadCount ? `${uploadCount} 个上传任务` : '上传队列为空' }}<ArrowUpRight aria-hidden="true" />
    </button>
  </section>
</template>

<style scoped>
.status-card{display:grid;gap:.35rem;width:100%;padding:.85rem;border:1px solid color-mix(in srgb,var(--accent-secondary) 28%,var(--border-default));border-radius:var(--radius);background:linear-gradient(145deg,color-mix(in srgb,var(--accent-secondary) 9%,var(--surface-raised)),var(--surface-raised));box-shadow:var(--shadow-sm);text-align:left}
.status-head{display:flex;align-items:center;gap:.5rem;font-size:.78rem}.status-head i{width:.55rem;height:.55rem;border-radius:50%;background:var(--state-warning)}.status-head i.connected{background:var(--accent-secondary);box-shadow:0 0 0 4px color-mix(in srgb,var(--accent-secondary) 16%,transparent)}.device,.storage{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--text-secondary);font-size:.7rem}.storage[data-state='DEGRADED'],.storage[data-state='UNAVAILABLE']{color:var(--state-warning)}.queue{display:flex;justify-content:space-between;align-items:center;width:100%;padding:.25rem 0 0;border:0;border-top:1px solid color-mix(in srgb,var(--border-default) 65%,transparent);background:transparent;color:var(--text-tertiary);font-size:.68rem;text-align:left}.queue svg{width:.85rem;height:.85rem;color:var(--accent-secondary)}
</style>
