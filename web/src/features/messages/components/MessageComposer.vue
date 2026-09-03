<script setup lang="ts">
import { LockKeyhole, LockOpen, Paperclip, RotateCcw, Send, Tags } from '@lucide/vue'
import { computed, ref } from 'vue'
import { Lifecycle } from '@/api/generated'
import { formatBytes } from '@/shared/utils/bytes'
import { isCodeContentType } from '../content/contentFormat'
import { useMessageComposer } from '../composables/useMessageComposer'
import ComposerEditor from './composer/ComposerEditor.vue'
import ComposerAttachments from './composer/ComposerAttachments.vue'
import ContentTypePicker from './ContentTypePicker.vue'
import RecipientPicker from './RecipientPicker.vue'

const props = defineProps<{ defaultLifecycle: Lifecycle }>()
const emit = defineEmits<{ sent: [] }>()
const fileInput = ref<HTMLInputElement>()
const composer = useMessageComposer(() => props.defaultLifecycle, () => emit('sent'))
const isCode = computed(() => isCodeContentType(composer.contentType.value))

function filesChanged(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.files) void composer.selectFiles(input.files)
  input.value = ''
}
</script>

<template>
  <section
    class="composer panel"
    aria-label="新内容"
  >
    <div class="composer-top">
      <ContentTypePicker v-model="composer.contentType.value" />
      <div class="composer-top-right">
        <RecipientPicker v-model="composer.selectedRecipient.value" />
        <button
          class="icon-tool"
          :class="{ on: composer.sensitive.value }"
          type="button"
          :aria-pressed="composer.sensitive.value"
          aria-label="敏感内容"
          title="敏感内容（详情中揭示后可见）"
          @click="composer.sensitive.value = !composer.sensitive.value"
        >
          <LockKeyhole v-if="composer.sensitive.value" />
          <LockOpen v-else />
        </button>
      </div>
    </div>

    <ComposerEditor
      v-model="composer.body.value"
      :code="isCode"
      :dragging="composer.dragging.value"
      @dragstart="composer.dragging.value = true"
      @dragend="composer.dragging.value = false"
      @drop="composer.dropFiles"
      @paste="composer.pasteFiles"
      @keydown="composer.onKeydown"
    />

    <ComposerAttachments
      :uploads="composer.selectedUploads.value"
      :blocking="composer.attachmentsBlocking.value"
      @remove="composer.removeSelected"
    />

    <div class="composer-toolbar">
      <input
        ref="fileInput"
        class="sr-only"
        type="file"
        multiple
        :disabled="!composer.storageAvailable.value"
        @change="filesChanged"
      >
      <button
        class="tool-button"
        type="button"
        aria-label="附件"
        :title="composer.storageAvailable.value ? '添加附件' : '存储服务暂时不可用'"
        :disabled="!composer.storageAvailable.value"
        @click="fileInput?.click()"
      >
        <Paperclip aria-hidden="true" /><span class="tool-label">附件</span>
      </button>

      <details
        v-if="composer.storageAvailable.value && composer.restorableUploads.value.length"
        class="popover restored-menu"
      >
        <summary
          class="tool-button"
          aria-label="已完成的上传"
          title="选用已完成的上传"
        >
          <RotateCcw aria-hidden="true" /><span class="count">{{ composer.restorableUploads.value.length }}</span>
        </summary>
        <div class="popover-panel">
          <h3 class="panel-title">
            已完成的上传
          </h3>
          <ul>
            <li
              v-for="item in composer.restorableUploads.value"
              :key="item.clientId"
            >
              <span>{{ item.filename }} · {{ formatBytes(item.size) }}</span>
              <button
                class="button"
                type="button"
                @click="composer.addRestored(item.clientId)"
              >
                选用
              </button>
            </li>
          </ul>
        </div>
      </details>

      <!-- Direct send cannot carry tags: render a truly disabled control (a
           <details> has no disabled contract) instead of the interactive
           picker. The kept draft reappears once the user switches back to
           sending to themselves. -->
      <details
        v-if="!composer.directMode.value"
        class="popover tag-picker"
      >
        <summary
          class="tool-button"
          aria-label="标签"
          title="标签"
        >
          <Tags aria-hidden="true" /><span class="tool-label">标签</span><span
            v-if="composer.selectedTags.value.length"
            class="count"
          >{{ composer.selectedTags.value.length }}</span>
        </summary>
        <div class="popover-panel tag-options">
          <p
            v-if="!composer.tags.data.value?.length"
            class="empty-note"
          >
            暂无标签，可在下方创建。
          </p>
          <label
            v-for="tag in composer.tags.data.value"
            :key="tag.id"
          >
            <input
              v-model="composer.selectedTags.value"
              type="checkbox"
              :value="tag.id"
            >
            <i :style="{ backgroundColor: tag.color }" />{{ tag.name }}
          </label>
          <div class="new-tag">
            <input
              v-model="composer.newTagName.value"
              class="input"
              maxlength="64"
              placeholder="新建标签"
              aria-label="新建标签名称"
            >
            <input
              v-model="composer.newTagColor.value"
              type="color"
              aria-label="标签颜色"
            >
            <button
              class="button"
              type="button"
              @click="composer.addTag"
            >
              添加
            </button>
          </div>
        </div>
      </details>
      <button
        v-else
        class="tool-button"
        type="button"
        disabled
        aria-label="标签"
        title="直发不带标签"
      >
        <Tags aria-hidden="true" /><span class="tool-label">标签</span><span
          v-if="composer.selectedTags.value.length"
          class="count"
        >{{ composer.selectedTags.value.length }}</span>
      </button>

      <label class="lifecycle-control">
        <span class="sr-only">保存位置</span>
        <select
          v-model="composer.lifecycle.value"
          :disabled="composer.directMode.value"
          title="保存位置"
        >
          <option :value="Lifecycle.TEMPORARY">临时</option>
          <option :value="Lifecycle.PERMANENT">长期</option>
        </select>
      </label>

      <span
        class="bytes"
        :class="{ error: composer.tooLarge.value }"
        title="正文上限 1 MB"
      >{{ formatBytes(composer.byteLength.value) }}</span>
      <button
        class="button primary send-button"
        type="button"
        :disabled="composer.sending.value || !composer.hasContent.value || composer.attachmentsBlocking.value || composer.tooLarge.value"
        @click="composer.directMode.value ? composer.submitDirect() : composer.submit()"
      >
        <Send aria-hidden="true" />{{ composer.directMode.value ? (composer.sending.value ? '直发中…' : '直接发送') : composer.sending.value ? '发送中…' : composer.failed.value ? '重试发送' : '发送' }}
      </button>
    </div>

    <div
      class="composer-feedback"
      aria-live="polite"
    >
      <p
        v-if="composer.directMode.value"
        class="muted direct-hint"
      >
        直发为独立临时副本，不带标签。
      </p>
      <p
        v-if="composer.lifecycle.value === Lifecycle.PERMANENT && !composer.selectedTags.value.length && !composer.directMode.value"
        class="warning"
      >
        长期内容尚未添加标签（仍可发送）。
      </p>
      <p
        v-if="composer.tooLarge.value"
        class="error"
        role="alert"
      >
        正文 UTF-8 大小不能超过 1 MB。
      </p>
      <p
        v-if="composer.error.value"
        class="error"
        role="alert"
      >
        {{ composer.error.value }}
      </p>
    </div>
  </section>
</template>

<style scoped>
.composer{position:relative;display:grid;border-radius:var(--radius-lg);box-shadow:var(--shadow-md);overflow:visible}
.composer-top{display:flex;align-items:center;justify-content:space-between;gap:.5rem;padding:.65rem .8rem 0}
.composer-top-right{display:flex;align-items:center;gap:.4rem}
.icon-tool{display:inline-grid;place-items:center;width:34px;height:34px;border:1px solid var(--border-default);border-radius:.6rem;background:var(--surface-raised);color:var(--text-secondary);cursor:pointer}.icon-tool svg{width:.95rem;height:.95rem}.icon-tool:hover{border-color:var(--border-strong);background:var(--surface-soft);color:var(--text-primary)}.icon-tool:focus-visible{outline:2px solid var(--focus-ring);outline-offset:2px}.icon-tool.on{border-color:var(--state-warning);color:var(--state-warning);background:color-mix(in srgb,var(--state-warning) 12%,var(--surface-raised))}
.composer-toolbar{display:flex;align-items:center;gap:.3rem;padding:.65rem .8rem}
.tool-button{display:inline-flex;align-items:center;justify-content:center;gap:.35rem;min-height:36px;border:0;border-radius:.55rem;padding:.4rem .55rem;background:transparent;color:var(--text-secondary);font-size:.8rem;font-weight:620;cursor:pointer}.tool-button>svg{width:1rem;height:1rem}.tool-button:hover{background:var(--surface-soft);color:var(--text-primary)}.tool-button:focus-visible{outline:2px solid var(--focus-ring);outline-offset:-2px}.tool-button:disabled{cursor:not-allowed;opacity:.55}
.lifecycle-control select{min-height:36px;border:0;border-radius:.55rem;padding:.4rem .45rem;background:transparent;color:var(--text-secondary);font-size:.8rem;font-weight:620;appearance:auto;cursor:pointer}.lifecycle-control select:hover{background:var(--surface-soft);color:var(--text-primary)}.lifecycle-control select:disabled{cursor:not-allowed;opacity:.55}
.count{display:grid;place-items:center;min-width:18px;height:18px;border-radius:999px;background:var(--accent-primary-soft);color:var(--accent-primary);font-size:.66rem}
.popover{position:relative}.popover>summary{list-style:none;cursor:pointer}.popover>summary::-webkit-details-marker{display:none}.popover[open]>summary{background:var(--surface-soft);color:var(--text-primary)}
.popover-panel{position:absolute;z-index:20;bottom:calc(100% + .55rem);left:0;width:max-content;min-width:210px;max-width:min(360px,calc(100vw - 2rem));padding:.65rem;border:1px solid var(--border-default);border-radius:var(--radius);background:var(--surface-raised);box-shadow:var(--shadow-floating)}
.panel-title{margin:0 0 .45rem;font-size:.72rem;text-transform:uppercase;letter-spacing:.08em;color:var(--text-tertiary)}
.tag-options{display:grid;gap:.25rem}.tag-options label{display:flex;align-items:center;gap:.45rem;padding:.35rem;border-radius:.4rem;font-size:.82rem}.tag-options label:hover{background:var(--surface-soft)}.tag-options i{width:.55rem;height:.55rem;border-radius:50%}.empty-note{margin:.2rem;color:var(--text-secondary);font-size:.78rem}
.new-tag{display:grid;grid-template-columns:minmax(0,1fr) 38px auto;gap:.4rem;margin-top:.45rem;padding-top:.5rem;border-top:1px solid var(--border-default)}.new-tag input[type=color]{width:38px;height:38px;padding:.1rem;border:1px solid var(--border-default);border-radius:var(--radius-sm);background:transparent}.new-tag .button{min-height:38px;padding:.35rem .65rem}
.restored-menu ul{list-style:none;margin:0;padding:0;display:grid;gap:.35rem}.restored-menu li{display:flex;align-items:center;justify-content:space-between;gap:.6rem;font-size:.76rem}.restored-menu li span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.restored-menu .button{min-height:32px;padding:.25rem .5rem;font-size:.7rem;white-space:nowrap}
.bytes{margin-left:auto;color:var(--text-tertiary);font-family:var(--font-mono);font-size:.65rem;white-space:nowrap}.bytes.error{color:var(--state-danger)}
.send-button{display:inline-flex;align-items:center;gap:.35rem;min-height:38px;padding:.45rem .9rem;border-radius:.65rem}.send-button svg{width:.9rem;height:.9rem}
.composer-feedback{display:grid;gap:.2rem;padding:0 .9rem}.composer-feedback:empty{display:none}.composer-feedback p{margin:0 0 .65rem;font-size:.76rem}.warning{color:var(--state-warning)}.direct-hint{color:var(--text-tertiary)}
@media(max-width:700px){.composer-top{padding:.55rem .6rem 0}.composer-toolbar{flex-wrap:wrap;padding:.55rem .6rem}.tool-label{display:none}.tool-button,.lifecycle-control select{padding-inline:.55rem}.bytes{order:5;margin-left:auto}.send-button{order:6;padding-inline:.8rem}.popover-panel{position:fixed;left:1rem;right:1rem;bottom:calc(72px + env(safe-area-inset-bottom));width:auto;min-width:0;max-width:none}.new-tag{grid-template-columns:minmax(0,1fr) 38px auto}}
@media(max-width:420px){.bytes{display:none}.send-button{margin-left:auto}}
</style>
