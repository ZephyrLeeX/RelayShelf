<script setup lang="ts">
import { Plus, Tags, X } from '@lucide/vue'
import { ref, watch } from 'vue'
import type { Message } from '@/api/generated'
import { displayError } from '@/shared/api/errors'
import { toast } from '@/shared/ui/toast'
import { useCreateTag, useTagsQuery } from '@/features/tags/queries'
import { mutationErrorMessage, useMessageMutation } from '../mutations'

const props = withDefaults(defineProps<{
  message: Message
  label?: string
  iconOnly?: boolean
}>(), { label: '标签', iconOnly: false })
const emit = defineEmits<{ saved: [] }>()
const open = ref(false)
const selected = ref<string[]>([])
const newTagName = ref('')
const newTagColor = ref('#3B8C6E')
const tags = useTagsQuery()
const createTag = useCreateTag()
const mutation = useMessageMutation()

function resetSelection() {
  selected.value = props.message.tags.map((tag) => tag.id)
}

watch(() => [props.message.id, props.message.version] as const, () => {
  if (!open.value) resetSelection()
}, { immediate: true })

function toggle() {
  if (!open.value) resetSelection()
  open.value = !open.value
}

async function addTag() {
  const name = newTagName.value.trim()
  if (!name) return
  try {
    const tag = await createTag.mutateAsync({ name, color: newTagColor.value })
    if (!selected.value.includes(tag.id)) selected.value.push(tag.id)
    newTagName.value = ''
    toast.success('标签已创建并选中')
  } catch (cause) {
    toast.error(displayError(cause))
  }
}

function save() {
  mutation.mutate({ type: 'tags', message: props.message, tagIds: [...selected.value] }, {
    onSuccess: () => {
      open.value = false
      toast.success('标签已更新')
      emit('saved')
    },
    onError: (cause) => toast.error(mutationErrorMessage(cause)),
  })
}
</script>

<template>
  <div
    class="message-tag-picker"
    :class="{ 'icon-mode': iconOnly }"
    @click.stop
  >
    <button
      class="tag-trigger"
      :class="{ 'icon-only': iconOnly }"
      type="button"
      :aria-label="label"
      :title="label"
      :aria-expanded="open"
      @click="toggle"
    >
      <Tags
        v-if="iconOnly"
        aria-hidden="true"
      />
      <Plus
        v-else
        aria-hidden="true"
      />
      <span v-if="!iconOnly">{{ label }}</span>
    </button>
    <div
      v-if="open"
      class="tag-popover"
      role="dialog"
      aria-label="选择消息标签"
    >
      <div class="popover-heading">
        <strong>消息标签</strong>
        <button
          type="button"
          aria-label="关闭标签选择"
          @click="open = false"
        >
          <X aria-hidden="true" />
        </button>
      </div>
      <div class="tag-options">
        <label
          v-for="tag in tags.data.value"
          :key="tag.id"
        >
          <input
            v-model="selected"
            type="checkbox"
            :value="tag.id"
          >
          <i :style="{ backgroundColor: tag.color }" />
          <span>{{ tag.name }}</span>
        </label>
        <p
          v-if="!tags.data.value?.length"
          class="empty-note"
        >
          暂无标签，可在下方创建。
        </p>
      </div>
      <div class="new-tag">
        <input
          v-model="newTagName"
          maxlength="64"
          placeholder="新建标签"
          aria-label="新建标签名称"
        >
        <input
          v-model="newTagColor"
          type="color"
          aria-label="标签颜色"
        >
        <button
          type="button"
          :disabled="createTag.isPending.value || !newTagName.trim()"
          @click="addTag"
        >
          添加
        </button>
      </div>
      <button
        class="button primary save-tags"
        type="button"
        :disabled="mutation.isPending.value"
        @click="save"
      >
        {{ mutation.isPending.value ? '保存中…' : '完成' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.message-tag-picker{position:relative;display:inline-flex;min-width:0}.tag-trigger{display:inline-flex;align-items:center;gap:.2rem;min-height:25px;border:1px dashed var(--border-strong);border-radius:999px;padding:.16rem .45rem;background:transparent;color:var(--text-secondary);font-size:.7rem;cursor:pointer}.tag-trigger:hover,.tag-trigger[aria-expanded=true]{border-color:var(--accent-primary);background:var(--accent-primary-soft);color:var(--accent-primary)}.tag-trigger svg{width:.72rem;height:.72rem}.tag-trigger.icon-only{display:inline-grid;place-items:center;width:34px;height:34px;border:0;border-radius:9px;padding:0}.tag-trigger.icon-only svg{width:1rem;height:1rem}.tag-trigger:focus-visible{outline:2px solid var(--focus-ring);outline-offset:2px}
.tag-popover{position:absolute;z-index:30;left:0;bottom:calc(100% + .45rem);display:grid;gap:.55rem;width:min(320px,calc(100vw - 2rem));max-height:min(420px,70vh);overflow:auto;padding:.7rem;border:1px solid var(--border-default);border-radius:var(--radius);background:var(--surface-raised);box-shadow:var(--shadow-floating);cursor:default}.popover-heading{display:flex;align-items:center;justify-content:space-between;gap:.5rem;font-size:.78rem}.popover-heading button{display:grid;place-items:center;width:28px;height:28px;border:0;border-radius:.4rem;background:transparent;color:var(--text-secondary)}.popover-heading button:hover{background:var(--surface-soft)}.popover-heading svg{width:.9rem}
.icon-mode .tag-popover{right:0;left:auto}
.tag-options{display:grid;gap:.2rem}.tag-options label{display:grid;grid-template-columns:auto .55rem minmax(0,1fr);align-items:center;gap:.45rem;padding:.35rem;border-radius:.4rem;font-size:.8rem}.tag-options label:hover{background:var(--surface-soft)}.tag-options i{width:.55rem;height:.55rem;border-radius:50%}.tag-options span{overflow-wrap:anywhere}.empty-note{margin:.2rem;color:var(--text-tertiary);font-size:.76rem}.new-tag{display:grid;grid-template-columns:minmax(0,1fr) 38px auto;gap:.35rem;padding-top:.5rem;border-top:1px solid var(--border-default)}.new-tag input:not([type=color]){min-width:0}.new-tag input[type=color]{width:38px;height:36px;padding:.1rem}.new-tag button{min-height:36px}.save-tags{justify-self:end;min-height:34px;padding:.35rem .7rem;font-size:.75rem}
@media(max-width:600px){.tag-popover,.icon-mode .tag-popover{position:fixed;left:.75rem;right:.75rem;bottom:calc(.75rem + env(safe-area-inset-bottom));width:auto;max-height:min(70vh,520px)}.new-tag{grid-template-columns:minmax(0,1fr) 38px auto}}
</style>
