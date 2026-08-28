<script setup lang="ts">
import { computed } from 'vue'
import { useRegisterSW } from 'virtual:pwa-register/vue'
import { hasActiveTransfers, reloadUnsafe, visibleUploads } from '@/features/uploads/store'

const { needRefresh, updateServiceWorker } = useRegisterSW({ immediate: true })
const paused = computed(() => visibleUploads.value.some((item) => item.status === 'PAUSED'))
const updateBlocked = computed(() => hasActiveTransfers.value || reloadUnsafe.value)
function update() { if (!updateBlocked.value) void updateServiceWorker(true) }
</script>

<template>
  <aside
    v-if="needRefresh"
    class="update-toast panel"
    role="status"
    aria-live="polite"
  >
    <div class="version-mark">
      NEW
    </div><div>
      <strong>发现新版本</strong><p v-if="hasActiveTransfers">
        当前有文件正在上传，上传完成后再刷新。
      </p><p v-else-if="reloadUnsafe">
        恢复记录当前不可用：仍有仅保存在本页的上传（暂停、待重试或尚未使用），现在刷新会丢失它们。请先完成或移除这些上传。
      </p><p v-else-if="paused">
        更新后需要重新选择暂停的文件继续上传。
      </p><p v-else>
        准备好后更新并刷新页面。
      </p>
    </div>
    <button
      class="button primary"
      type="button"
      :disabled="updateBlocked"
      @click="update"
    >
      更新并刷新
    </button>
  </aside>
</template>

<style scoped>
.update-toast{position:fixed;z-index:80;right:1rem;bottom:1rem;display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.8rem;width:min(560px,calc(100vw - 2rem));padding:.85rem;box-shadow:var(--shadow)}.version-mark{display:grid;place-items:center;width:42px;height:42px;border-radius:12px;background:var(--accent-soft);color:var(--accent-strong);font:800 .65rem var(--font-mono);letter-spacing:.08em}.update-toast p{margin:.15rem 0 0;color:var(--muted);font-size:.8rem}.update-toast .button{white-space:nowrap}@media(max-width:600px){.update-toast{bottom:4.4rem;grid-template-columns:auto 1fr}.update-toast .button{grid-column:1/-1;width:100%}}
</style>
