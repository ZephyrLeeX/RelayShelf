<script setup lang="ts">
defineProps<{ code: boolean }>()
const body = defineModel<string>({ required: true })
defineEmits<{
  keydown: [event: KeyboardEvent]
  paste: [event: ClipboardEvent]
}>()
</script>

<template>
  <div
    class="editor"
  >
    <label
      class="sr-only"
      for="composer-body"
    >正文</label>
    <textarea
      id="composer-body"
      v-model="body"
      rows="4"
      :class="{ code }"
      placeholder="写下要在其他设备取回的内容…"
      @keydown="$emit('keydown', $event)"
      @paste="$emit('paste', $event)"
    />
  </div>
</template>

<style scoped>
.editor{position:relative;border-bottom:1px solid var(--border-default);transition:background .14s,border-color .14s}
textarea{display:block;resize:vertical;width:100%;min-height:116px;border:0;padding:.9rem 1rem .8rem;background:transparent;line-height:1.55;outline:0}.code{font-family:var(--font-mono);color:var(--content-code)}
@media(max-width:600px){textarea{min-height:104px;padding:.8rem .85rem}}
</style>
