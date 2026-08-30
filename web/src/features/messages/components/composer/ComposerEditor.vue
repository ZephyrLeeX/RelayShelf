<script setup lang="ts">
import type { ComposerMode } from '../../composables/useMessageComposer'

defineProps<{ mode: ComposerMode; dragging: boolean }>()
const body = defineModel<string>({ required: true })
defineEmits<{
  keydown: [event: KeyboardEvent]
  paste: [event: ClipboardEvent]
  dragstart: []
  dragend: []
  drop: [event: DragEvent]
}>()
</script>

<template>
  <div
    class="editor"
    :class="{ dragging }"
    @dragenter.prevent="$emit('dragstart')"
    @dragover.prevent="$emit('dragstart')"
    @dragleave.prevent="$emit('dragend')"
    @drop.prevent="$emit('drop', $event)"
  >
    <label
      class="sr-only"
      for="composer-body"
    >正文</label>
    <textarea
      id="composer-body"
      v-model="body"
      rows="4"
      :class="{ code: mode === 'code' }"
      placeholder="写下要在其他设备取回的内容…"
      @keydown="$emit('keydown', $event)"
      @paste="$emit('paste', $event)"
    />
    <span
      v-if="dragging"
      class="drop-prompt"
    >松开以添加到本条内容</span>
  </div>
</template>

<style scoped>
.editor{position:relative;border-bottom:1px solid var(--border-default);transition:background .14s,border-color .14s}.editor.dragging{background:var(--accent-primary-soft);border-color:var(--accent-primary)}
textarea{display:block;resize:vertical;width:100%;min-height:116px;border:0;padding:1.1rem 1.15rem .9rem;background:transparent;line-height:1.55;outline:0}.code{font-family:var(--font-mono);color:var(--content-code)}
.drop-prompt{position:absolute;inset:.55rem;display:grid;place-items:center;border:1px dashed var(--accent-primary);border-radius:var(--radius-sm);background:var(--accent-primary-soft);color:var(--accent-primary-hover);font-size:.82rem;font-weight:650;pointer-events:none}
@media(max-width:600px){textarea{min-height:104px;padding:1rem}}
</style>
