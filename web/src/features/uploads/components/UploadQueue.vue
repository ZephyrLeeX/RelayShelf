<script setup lang="ts">
import { visibleUploads, uploadState } from '../store'
import UploadItem from './UploadItem.vue'
import ResumeUploads from './ResumeUploads.vue'

defineEmits<{ close: [] }>()
</script>

<template>
  <Teleport to="body">
    <div
      class="queue-backdrop"
      @click.self="$emit('close')"
    >
      <aside
        class="queue panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="queue-title"
      >
        <header>
          <div>
            <span class="eyebrow">Transfer shelf</span><h2 id="queue-title">
              上传任务
            </h2>
          </div><button
            class="button close"
            aria-label="关闭上传任务"
            @click="$emit('close')"
          >
            ×
          </button>
        </header>
        <ResumeUploads />
        <p
          v-if="!visibleUploads.length"
          class="empty"
        >
          当前没有上传任务。
        </p>
        <UploadItem
          v-for="item in visibleUploads"
          :key="item.clientId"
          :item="item"
        />
        <p
          v-if="uploadState.ledgerWarning"
          class="warning"
        >
          本浏览器无法保存断点恢复信息；上传本身不受影响，但刷新后无法恢复未完成的上传。
        </p>
        <p class="privacy">
          文件内容仅在当前页面内存中使用；刷新后需要重新选择未完成的文件。
        </p>
      </aside>
    </div>
  </Teleport>
</template>

<style scoped>
.queue-backdrop{position:fixed;inset:0;z-index:70;background:rgb(16 24 21 / .28);display:flex;justify-content:flex-end}.queue{width:min(430px,94vw);height:100%;overflow:auto;border-radius:var(--radius-lg) 0 0 var(--radius-lg);padding:1rem 1.15rem;box-shadow:var(--shadow)}header{display:flex;align-items:flex-start;justify-content:space-between;border-bottom:1px solid var(--border);padding-bottom:.8rem;margin-bottom:.3rem}h2{margin:.15rem 0;font-size:1.15rem}.eyebrow{font:700 .67rem/1 var(--font-mono);letter-spacing:.13em;text-transform:uppercase;color:var(--accent-strong)}.close{font-size:1.25rem}.empty{padding:2.2rem .5rem;text-align:center;color:var(--muted)}.warning{margin:.5rem 0 0;padding:.55rem .65rem;border-radius:var(--radius-sm);background:var(--surface-soft);color:#9a6414;font-size:.76rem;line-height:1.5}.privacy{font-size:.72rem;line-height:1.5;color:var(--muted);border-top:1px solid var(--border);padding-top:.8rem}
@media(prefers-reduced-motion:no-preference){.queue{animation:queue-in .16s ease-out}@keyframes queue-in{from{transform:translateX(20px);opacity:.75}}}@media(max-width:600px){.queue{width:100%;border-radius:0}}
</style>
