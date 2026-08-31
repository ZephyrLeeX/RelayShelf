<script setup lang="ts">
import { MoreHorizontal, Paperclip, Tags } from '@lucide/vue'
import { ref } from 'vue'
import { Lifecycle } from '@/api/generated'
import { formatBytes } from '@/shared/utils/bytes'
import { useMessageComposer, type ComposerMode } from '../composables/useMessageComposer'
import ComposerEditor from './composer/ComposerEditor.vue'
import ComposerAttachments from './composer/ComposerAttachments.vue'

const props = defineProps<{ defaultLifecycle: Lifecycle }>()
const emit = defineEmits<{ sent: [] }>()
const fileInput = ref<HTMLInputElement>()
const composer = useMessageComposer(() => props.defaultLifecycle, () => emit('sent'))

function filesChanged(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.files) void composer.selectFiles(input.files)
  input.value = ''
}

function setMode(mode: ComposerMode) {
  composer.mode.value = mode
}
</script>

<template>
  <section
    class="composer panel"
    aria-labelledby="composer-title"
  >
    <header class="composer-heading">
      <div>
        <span
          class="route-mark"
          aria-hidden="true"
        />
        <h2 id="composer-title">
          新内容
        </h2>
      </div>
      <span class="shortcut">Ctrl/⌘ + Enter</span>
    </header>

    <ComposerEditor
      v-model="composer.body.value"
      :mode="composer.mode.value"
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
        @change="filesChanged"
      >
      <button
        class="tool-button attach"
        type="button"
        @click="fileInput?.click()"
      >
        <Paperclip aria-hidden="true" />文件
      </button>

      <details class="popover tag-picker">
        <summary class="tool-button">
          <Tags aria-hidden="true" />标签<span
            v-if="composer.selectedTags.value.length"
            class="count"
          >{{ composer.selectedTags.value.length }}</span>
        </summary>
        <div class="popover-panel tag-options">
          <p
            v-if="!composer.tags.data.value?.length"
            class="empty-note"
          >
            暂无标签，可在高级选项中创建。
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
        </div>
      </details>

      <label class="lifecycle-control">
        <span class="sr-only">保存位置</span>
        <select
          v-model="composer.lifecycle.value"
          :disabled="composer.directMode.value"
        >
          <option :value="Lifecycle.TEMPORARY">临时</option>
          <option :value="Lifecycle.PERMANENT">长期</option>
        </select>
      </label>

      <details class="popover advanced-menu">
        <summary
          class="tool-button"
          aria-label="高级选项"
        >
          <MoreHorizontal aria-hidden="true" />
        </summary>
        <div class="popover-panel advanced-panel">
          <section>
            <h3>正文格式</h3>
            <div
              class="modes"
              role="group"
              aria-label="正文格式"
            >
              <button
                v-for="item in (['text','markdown','code'] as const)"
                :key="item"
                type="button"
                :class="{ active: composer.mode.value === item }"
                @click="setMode(item)"
              >
                {{ item === 'text' ? '纯文本' : item === 'markdown' ? 'Markdown' : 'Code' }}
              </button>
            </div>
          </section>

          <label class="toggle">
            <input
              v-model="composer.sensitive.value"
              type="checkbox"
            >
            <span><strong>敏感内容</strong><small>正文需在详情中显式揭示</small></span>
          </label>

          <label class="field direct-send">
            <span>直接发送给其他用户</span>
            <input
              v-model="composer.directRecipient.value"
              class="input"
              placeholder="接收者用户 ID（UUID）"
              maxlength="64"
            >
          </label>
          <p
            v-if="composer.directMode.value"
            class="muted direct-note"
          >
            直发会创建独立临时副本，不保留你的副本，也不带标签。
          </p>

          <section class="new-tag-section">
            <h3>创建标签</h3>
            <div class="new-tag">
              <input
                v-model="composer.newTagName.value"
                class="input"
                maxlength="64"
                placeholder="标签名称"
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
          </section>

          <section
            v-if="composer.restorableUploads.value.length"
            class="restored"
            aria-label="已完成的上传"
          >
            <h3>已完成的上传</h3>
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
                  添加到本条
                </button>
              </li>
            </ul>
          </section>
        </div>
      </details>

      <span
        class="bytes"
        :class="{ error: composer.tooLarge.value }"
      >
        {{ formatBytes(composer.byteLength.value) }} / 1 MB
      </span>
      <button
        class="button primary send-button"
        type="button"
        :disabled="composer.sending.value || !composer.hasContent.value || composer.attachmentsBlocking.value || composer.tooLarge.value"
        @click="composer.directMode.value ? composer.submitDirect() : composer.submit()"
      >
        {{ composer.directMode.value ? (composer.sending.value ? '直发中…' : '直接发送') : composer.sending.value ? '发送中…' : composer.failed.value ? '重试发送' : '发送' }}
      </button>
    </div>

    <div
      class="composer-feedback"
      aria-live="polite"
    >
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
.composer{position:relative;display:grid;border-radius:var(--radius-lg);box-shadow:var(--shadow-md);overflow:visible}.composer-heading{display:flex;align-items:center;justify-content:space-between;padding:.7rem 1rem .55rem}.composer-heading>div{display:flex;align-items:center;gap:.5rem}.route-mark{width:9px;height:9px;border-radius:50%;background:var(--accent-secondary);box-shadow:0 0 0 4px color-mix(in srgb,var(--accent-secondary) 13%,transparent)}h2{margin:0;font-size:.88rem;letter-spacing:.01em}.shortcut{color:var(--text-tertiary);font-family:var(--font-mono);font-size:.67rem}
.composer-toolbar{display:flex;align-items:center;gap:.35rem;padding:.7rem .8rem}.tool-button,.lifecycle-control select{display:inline-flex;align-items:center;justify-content:center;gap:.35rem;min-height:36px;border:0;border-radius:.55rem;padding:.4rem .62rem;background:transparent;color:var(--text-secondary);font-size:.8rem;font-weight:620}.tool-button>svg{width:1rem;height:1rem}.tool-button:hover,.lifecycle-control select:hover{background:var(--surface-soft);color:var(--text-primary)}.attach{color:var(--accent-secondary)}.count{display:grid;place-items:center;min-width:18px;height:18px;border-radius:999px;background:var(--accent-primary-soft);color:var(--accent-primary);font-size:.66rem}.lifecycle-control select{appearance:auto;cursor:pointer}.lifecycle-control select:disabled{cursor:not-allowed;opacity:.55}.popover{position:relative}.popover>summary{list-style:none;cursor:pointer}.popover>summary::-webkit-details-marker{display:none}.popover[open]>summary{background:var(--surface-soft);color:var(--text-primary)}.popover-panel{position:absolute;z-index:20;bottom:calc(100% + .55rem);left:0;width:max-content;min-width:210px;max-width:min(360px,calc(100vw - 2rem));padding:.65rem;border:1px solid var(--border-default);border-radius:var(--radius);background:var(--surface-raised);box-shadow:var(--shadow-floating)}.tag-options{display:grid;gap:.25rem}.tag-options label{display:flex;align-items:center;gap:.45rem;padding:.35rem;border-radius:.4rem;font-size:.82rem}.tag-options label:hover{background:var(--surface-soft)}.tag-options i{width:.55rem;height:.55rem;border-radius:50%}.empty-note{margin:.2rem;color:var(--text-secondary);font-size:.78rem}.advanced-menu{margin-right:auto}.advanced-panel{left:auto;right:0;width:min(340px,calc(100vw - 2rem));display:grid;gap:.8rem}.advanced-panel section>h3,.restored h3{margin:0 0 .45rem;font-size:.72rem;text-transform:uppercase;letter-spacing:.08em;color:var(--text-tertiary)}.modes{display:grid;grid-template-columns:repeat(3,1fr);padding:.2rem;border-radius:.65rem;background:var(--surface-soft)}.modes button{border:0;border-radius:.5rem;padding:.42rem;background:transparent;font-size:.77rem}.modes .active{background:var(--surface-raised);box-shadow:var(--shadow-sm);color:var(--accent-primary)}.toggle{display:flex;align-items:flex-start;gap:.55rem}.toggle span{display:grid}.toggle strong{font-size:.82rem}.toggle small{color:var(--text-secondary);font-size:.71rem}.direct-send{font-size:.76rem}.direct-note{margin:-.4rem 0 0;font-size:.72rem}.new-tag{display:grid;grid-template-columns:1fr 38px auto;gap:.4rem}.new-tag input[type=color]{width:38px;height:38px;padding:.1rem;border:1px solid var(--border-default);border-radius:var(--radius-sm);background:transparent}.new-tag .button{min-height:38px;padding:.35rem .65rem}.restored ul{list-style:none;margin:0;padding:0;display:grid;gap:.35rem}.restored li{display:flex;align-items:center;justify-content:space-between;gap:.6rem;font-size:.76rem}.restored li span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.restored .button{min-height:32px;padding:.25rem .5rem;font-size:.7rem;white-space:nowrap}.bytes{color:var(--text-tertiary);font-family:var(--font-mono);font-size:.65rem;white-space:nowrap}.send-button{min-height:38px;padding:.45rem 1rem;border-radius:.65rem}.composer-feedback{display:grid;gap:.2rem;padding:0 .9rem}.composer-feedback:empty{display:none}.composer-feedback p{margin:0 0 .65rem;font-size:.76rem}.warning{color:var(--state-warning)}
@media(max-width:700px){.composer-heading{padding:.65rem .85rem .45rem}.shortcut{display:none}.composer-toolbar{flex-wrap:wrap;padding:.6rem}.advanced-menu{margin-right:0}.bytes{order:5;margin-left:auto}.send-button{order:6}.popover-panel{position:fixed;left:1rem;right:1rem;bottom:calc(72px + env(safe-area-inset-bottom));width:auto;max-width:none}.advanced-panel{width:auto}.composer-feedback{padding:0 .7rem}}
@media(max-width:420px){.bytes{display:none}.send-button{margin-left:auto}.lifecycle-control select,.tool-button{padding-inline:.5rem}}
</style>
