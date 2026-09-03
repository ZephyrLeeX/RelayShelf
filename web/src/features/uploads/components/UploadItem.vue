<script setup lang="ts">
import { ref } from 'vue'
import { formatBytes } from '@/shared/utils/bytes'
import { uploadStatusLabel } from '../labels'
import { uploadManager } from '../manager'
import type { UploadItem } from '../types'
import { storageAvailable } from '@/features/storage/runtime'

const props = defineProps<{ item: UploadItem }>()
const picker = ref<HTMLInputElement>()
// Finalization cannot be aborted mid-Complete, so COMPLETING deliberately
// offers no Pause; the button must not promise an action it cannot perform.
const pausable = ['QUEUED', 'CREATING', 'UPLOADING']

function continueUpload() {
  if (props.item.file) void uploadManager.resume(props.item.clientId)
  else picker.value?.click()
}
function selected(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (file) void uploadManager.resume(props.item.clientId, file)
  ;(event.target as HTMLInputElement).value = ''
}
</script>

<template>
  <article class="upload-item">
    <div class="upload-main">
      <strong :title="item.filename">{{ item.filename }}</strong>
      <span class="state">{{ uploadStatusLabel(item.status) }}</span>
      <span class="size">{{ formatBytes(item.sentBytes) }} / {{ formatBytes(item.size) }}</span>
    </div>
    <progress
      :value="item.progress"
      max="1"
      :aria-label="`${item.filename} 上传进度`"
    />
    <p
      v-if="item.status === 'PAUSED' && !item.file"
      class="hint"
    >
      重新选择原文件后，将从服务器已完成的分片继续。
    </p>
    <p
      v-if="item.status === 'FAILED' || item.status === 'EXPIRED'"
      class="error"
      role="alert"
    >
      {{ item.error }}
    </p>
    <div class="actions">
      <button
        v-if="pausable.includes(item.status)"
        class="button"
        type="button"
        @click="uploadManager.pause(item.clientId)"
      >
        暂停
      </button>
      <button
        v-if="item.status === 'PAUSED'"
        class="button"
        type="button"
        :disabled="!storageAvailable"
        :title="storageAvailable ? undefined : '存储服务暂时不可用'"
        @click="continueUpload"
      >
        {{ item.file ? '继续' : '选择原文件' }}
      </button>
      <button
        v-if="item.status === 'FAILED' && item.retryable"
        class="button"
        type="button"
        :disabled="!storageAvailable"
        :title="storageAvailable ? undefined : '存储服务暂时不可用'"
        @click="uploadManager.retry(item.clientId)"
      >
        重试
      </button>
      <button
        v-if="item.status === 'FAILED' && item.file"
        class="button"
        type="button"
        :disabled="!storageAvailable"
        :title="storageAvailable ? undefined : '存储服务暂时不可用'"
        @click="uploadManager.reupload(item.clientId)"
      >
        重新上传
      </button>
      <button
        class="button quiet"
        type="button"
        @click="uploadManager.remove(item.clientId)"
      >
        移除
      </button>
    </div>
    <input
      ref="picker"
      class="sr-only"
      type="file"
      :disabled="!storageAvailable"
      @change="selected"
    >
  </article>
</template>

<style scoped>
.upload-item{display:grid;gap:.45rem;padding:.8rem 0;border-bottom:1px solid var(--border)}.upload-item:last-child{border:0}.upload-main{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:.15rem .6rem}.upload-main strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.state{font:700 .68rem/1 var(--font-mono);letter-spacing:.05em;color:var(--accent-strong)}.size{grid-column:1/-1;color:var(--muted);font-size:.76rem}progress{width:100%;height:6px;accent-color:var(--accent)}.hint,.error{margin:0;font-size:.78rem}.hint{color:var(--muted)}.actions{display:flex;justify-content:flex-end;gap:.35rem}.actions .button{min-height:34px;padding:.3rem .55rem;font-size:.78rem}.quiet{color:var(--muted)}
</style>
