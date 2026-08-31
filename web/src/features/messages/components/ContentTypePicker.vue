<script setup lang="ts">
import { ChevronDown } from '@lucide/vue'
import { computed, ref } from 'vue'
import { useDismiss } from '@/shared/composables/useDismiss'
import { CONTENT_TYPES, type ContentTypeId } from '../content/contentFormat'

/**
 * Compact single-purpose content type selector shared by the Composer and the
 * detail editor. Selecting Shell/Python/Java/… serializes to a Markdown
 * fenced block at submit time; the textarea itself never shows the fence.
 */
const contentType = defineModel<ContentTypeId>({ required: true })
defineProps<{ disabled?: boolean }>()

const open = ref(false)
const root = ref<HTMLElement>()
useDismiss(open, root)

const current = computed(() => CONTENT_TYPES.find((type) => type.id === contentType.value) ?? CONTENT_TYPES[0])
const groups = computed(() => [
  { key: 'basic', label: '文本', items: CONTENT_TYPES.filter((type) => type.kind !== 'code') },
  { key: 'code', label: '代码', items: CONTENT_TYPES.filter((type) => type.kind === 'code') },
])

function select(id: ContentTypeId) {
  contentType.value = id
  open.value = false
}
</script>

<template>
  <div
    ref="root"
    class="content-type-picker"
  >
    <button
      class="type-trigger"
      type="button"
      :disabled="disabled"
      :aria-expanded="open"
      aria-haspopup="listbox"
      aria-label="内容类型"
      :title="`内容类型：${current.label}`"
      @click="open = !open"
    >
      <span
        class="type-chip"
        :class="{ code: current.kind === 'code' }"
      >{{ current.shortLabel }}</span>
      <span class="type-name">{{ current.label }}</span>
      <ChevronDown
        class="chevron"
        aria-hidden="true"
      />
    </button>
    <div
      v-if="open"
      class="type-menu"
      role="listbox"
      aria-label="内容类型"
    >
      <template
        v-for="group in groups"
        :key="group.key"
      >
        <p
          v-if="group.label"
          class="group-label"
        >
          {{ group.label }}
        </p>
        <button
          v-for="item in group.items"
          :key="item.id"
          type="button"
          role="option"
          :aria-label="item.label"
          :aria-selected="item.id === contentType"
          :class="{ selected: item.id === contentType }"
          :title="item.hint"
          @click="select(item.id)"
        >
          <span
            class="type-chip"
            :class="{ code: item.kind === 'code' }"
          >{{ item.shortLabel }}</span>
          <span class="type-name">{{ item.label }}</span>
        </button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.content-type-picker{position:relative}.type-trigger{display:inline-flex;align-items:center;gap:.4rem;min-height:34px;border:1px solid var(--border-default);border-radius:.6rem;padding:.28rem .55rem;background:var(--surface-raised);color:var(--text-primary);font-size:.78rem;font-weight:650;cursor:pointer}.type-trigger:hover{border-color:var(--border-strong);background:var(--surface-soft)}.type-trigger:disabled{cursor:not-allowed;opacity:.55}.type-trigger:focus-visible{outline:2px solid var(--focus-ring);outline-offset:2px}
.type-chip{display:inline-grid;place-items:center;min-width:30px;padding:.1rem .28rem;border-radius:.35rem;background:var(--surface-soft);color:var(--text-secondary);font-family:var(--font-mono);font-size:.64rem;font-weight:750;letter-spacing:.04em}.type-chip.code{background:color-mix(in srgb,var(--content-code) 13%,var(--surface-soft));color:var(--content-code)}
.type-name{max-width:9rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.chevron{width:.85rem;height:.85rem;color:var(--text-tertiary)}
.type-menu{position:absolute;z-index:30;top:calc(100% + .4rem);left:0;display:grid;gap:.1rem;width:max-content;max-width:min(280px,calc(100vw - 2rem));max-height:min(300px,60vh);overflow:auto;padding:.4rem;border:1px solid var(--border-default);border-radius:var(--radius-sm);background:var(--surface-raised);box-shadow:var(--shadow-floating)}
.group-label{margin:.25rem .3rem .1rem;color:var(--text-tertiary);font-size:.64rem;font-weight:700;letter-spacing:.08em;text-transform:uppercase}
.type-menu button{display:flex;align-items:center;gap:.45rem;width:100%;min-height:34px;border:0;border-radius:.45rem;padding:.3rem .4rem;background:transparent;color:var(--text-primary);font-size:.78rem;text-align:left;cursor:pointer}.type-menu button:hover{background:var(--surface-soft)}.type-menu button.selected{background:var(--accent-primary-soft);color:var(--accent-primary);font-weight:700}.type-menu button:focus-visible{outline:2px solid var(--focus-ring);outline-offset:-2px}
@media(max-width:480px){.type-trigger .type-name{display:none}.type-trigger{padding-inline:.45rem}}
</style>
