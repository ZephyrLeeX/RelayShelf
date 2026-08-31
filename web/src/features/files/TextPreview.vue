<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import type { AttachmentSummary } from '@/api/generated'
import { downloadURL } from './preview'

const props = defineProps<{ file: AttachmentSummary }>()
const content = ref('')
const pending = ref(true)
const error = ref('')
const truncated = ref(false)
let controller: AbortController | undefined

watch(() => props.file.id, async () => {
  controller?.abort(); controller = new AbortController(); pending.value = true; error.value = ''; content.value = ''
  try {
    const response = await fetch(downloadURL(props.file.id), { credentials: 'include', headers: { Range: 'bytes=0-1048575' }, signal: controller.signal })
    if (!response.ok && response.status !== 206) throw new Error('preview unavailable')
    const bytes = await response.arrayBuffer()
    content.value = new TextDecoder().decode(bytes)
    truncated.value = response.status === 206 || props.file.sizeBytes > bytes.byteLength
  } catch (cause) { if (!(cause instanceof DOMException && cause.name === 'AbortError')) error.value = '文本预览暂不可用，请下载后查看。' }
  finally { pending.value = false }
}, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <div class="text-preview">
    <p v-if="pending">
      正在读取前 1 MB…
    </p><p
      v-else-if="error"
      class="error"
    >
      {{ error }}
    </p><template v-else>
      <pre><code>{{ content }}</code></pre><p
        v-if="truncated"
        class="notice"
      >
        仅显示前 1 MB，下载文件可查看完整内容。
      </p>
    </template>
  </div>
</template>

<style scoped>.text-preview{height:100%;overflow:auto}.text-preview pre{margin:0;padding:1rem;min-height:100%;white-space:pre-wrap;overflow-wrap:anywhere;background:#17211e;color:#edf5f1;font:13px/1.55 var(--font-mono)}.notice{position:sticky;bottom:0;margin:0;padding:.65rem 1rem;background:#fff3cd;color:#76540b}</style>
