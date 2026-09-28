<script setup lang="ts">
import { nextTick, ref, useId } from 'vue'
import SafeMarkdown from '../SafeMarkdown.vue'
const props = withDefaults(defineProps<{ id?: string; code: boolean; markdown?: boolean; formatLocked?: boolean; required?: boolean; rows?: number; shortcut?: string }>(), { id: undefined, rows: 4, shortcut: '发送' })
const body = defineModel<string>({ required: true })
const emit = defineEmits<{ keydown: [event: KeyboardEvent]; paste: [event: ClipboardEvent]; format: [] }>()
const editorId = useId()
const input = ref<HTMLTextAreaElement>()
const preview = ref(false)
const composing = ref(false)
const formats = ['加粗', '斜体', '列表', '链接', '引用', '代码块']
async function format(kind: string) {
  if (!input.value) return
  const start = input.value.selectionStart
  const end = input.value.selectionEnd
  const selected = body.value.slice(start, end) || '文本'
  let value = selected
  if (kind === '加粗') value = `**${selected}**`
  if (kind === '斜体') value = `*${selected}*`
  if (kind === '链接') value = `[${selected}](https://example.com)`
  if (kind === '列表' || kind === '引用') value = '\n' + selected.split('\n').map(line => `${kind === '列表' ? '- ' : '> '}${line}`).join('\n') + '\n'
  if (kind === '代码块') {
    const fence = '`'.repeat(Math.max(3, ...[...selected.matchAll(/`+/g)].map(match => match[0].length + 1)))
    value = `\n${fence}\n${selected}\n${fence}\n`
  }
  body.value = body.value.slice(0, start) + value + body.value.slice(end)
  emit('format')
  await nextTick()
  input.value?.focus()
  input.value?.setSelectionRange(start, start + value.length)
}
function key(event: KeyboardEvent) {
  if (composing.value || event.isComposing || event.keyCode === 229) return
  emit('keydown', event)
}
</script>

<template>
  <div class="editor">
    <div
      v-if="!code && (!formatLocked || markdown)"
      class="format-toolbar"
      role="group"
      aria-label="文本格式"
    >
      <button
        v-for="kind in formats"
        :key="kind"
        type="button"
        :disabled="preview"
        @click="format(kind)"
      >
        {{ kind }}
      </button>
      <button
        type="button"
        :aria-pressed="preview"
        @click="preview = !preview"
      >
        {{ preview ? '继续编辑' : '预览' }}
      </button>
    </div>
    <label
      class="sr-only"
      :for="props.id ?? editorId"
    >正文</label>
    <textarea
      v-show="!preview"
      :id="props.id ?? editorId"
      ref="input"
      v-model="body"
      :rows="rows"
      :required="required"
      :class="{ code }"
      placeholder="写下要在其他设备取回的内容…"
      @compositionstart="composing = true"
      @compositionend="composing = false"
      @keydown="key"
      @paste="$emit('paste', $event)"
    />
    <div
      v-if="preview"
      class="editor-preview"
    >
      <SafeMarkdown
        v-if="markdown"
        :source="body"
      />
      <pre v-else>{{ body || '暂无内容' }}</pre>
    </div>
    <small>Enter 换行 · Ctrl / ⌘ + Enter {{ shortcut }}<template v-if="markdown"> · Markdown 格式，可预览</template></small>
  </div>
</template>

<style scoped>
.editor{position:relative;min-width:0;border-bottom:1px solid var(--border-default)}
.format-toolbar{display:flex;flex-wrap:wrap;gap:.25rem;padding:.5rem .75rem;background:var(--surface-soft);border-radius:8px}
.format-toolbar button{border:0;border-radius:6px;padding:.4rem .5rem;background:transparent;color:var(--text-secondary);cursor:pointer}.format-toolbar button:hover,.format-toolbar button[aria-pressed=true]{background:var(--accent-primary-soft);color:var(--accent-primary)}.format-toolbar button:focus-visible,textarea:focus-visible{outline:2px solid var(--focus-ring);outline-offset:-2px}.format-toolbar button:disabled{opacity:.5}
textarea{box-sizing:border-box;display:block;resize:vertical;width:100%;min-height:116px;border:0;padding:.9rem 1rem .8rem;background:transparent;line-height:1.55}.code{font-family:var(--font-mono);color:var(--content-code)}
.editor-preview{padding:1rem;max-height:400px;overflow:auto}.editor-preview pre{white-space:pre-wrap;overflow-wrap:anywhere}small{display:block;padding:.35rem .85rem;color:var(--text-tertiary);font-size:.7rem}
@media(max-width:600px){textarea{min-height:104px;padding:.8rem .85rem}.format-toolbar button{min-height:36px}}
</style>
