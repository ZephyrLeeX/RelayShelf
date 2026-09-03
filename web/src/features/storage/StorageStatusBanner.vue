<script setup lang="ts">
import { computed } from 'vue'
import { storageRuntimeStatus } from './runtime'

const full = computed(() => storageRuntimeStatus.value?.reason === 'NAS_FULL')
</script>

<template>
  <aside
    v-if="storageRuntimeStatus && !storageRuntimeStatus.healthy"
    class="storage-banner"
    :class="{ full }"
    role="alert"
  >
    <strong>{{ full ? '存储空间已满' : '存储服务暂时不可用' }}</strong>
    <span v-if="full">当前无法上传新的文件。已有文本内容仍可正常使用。</span>
    <span v-else>文件上传、下载和预览当前不可用。系统正在等待存储恢复；不依赖文件存储的功能仍可继续使用。</span>
  </aside>
</template>

<style scoped>
.storage-banner{display:flex;align-items:center;gap:.65rem;padding:.7rem clamp(.8rem,2vw,1.5rem);border-bottom:1px solid color-mix(in srgb,var(--state-warning) 45%,var(--border-default));background:color-mix(in srgb,var(--state-warning) 13%,var(--surface-raised));color:var(--text-primary);font-size:.82rem}.storage-banner strong{flex:none;color:var(--state-warning)}.storage-banner.full{border-color:color-mix(in srgb,var(--state-danger) 45%,var(--border-default));background:color-mix(in srgb,var(--state-danger) 11%,var(--surface-raised))}.storage-banner.full strong{color:var(--state-danger)}
@media(max-width:700px){.storage-banner{align-items:flex-start;flex-direction:column;gap:.2rem;padding:.65rem .8rem;font-size:.76rem;line-height:1.4}}
</style>
