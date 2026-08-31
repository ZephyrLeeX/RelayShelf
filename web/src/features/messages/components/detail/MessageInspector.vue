<script setup lang="ts">
import { Archive, CalendarPlus, Copy, Forward, LockKeyhole, MoreHorizontal, Paperclip, Pencil, Star, Trash2, X } from '@lucide/vue'
import { defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { BodyFormat } from '@/api/generated'
import { formatBytes } from '@/shared/utils/bytes'
import TagChip from '@/shared/ui/TagChip.vue'
import AttachmentList from '../AttachmentList.vue'
import SafeMarkdown from '../SafeMarkdown.vue'
import { useMessageDetailController } from '../../composables/useMessageDetailController'

const AttachmentViewer = defineAsyncComponent(() => import('@/features/files/AttachmentViewer.vue'))
const props = defineProps<{ id: string }>()
const emit = defineEmits<{ close: [] }>()
const tagEditing = ref(false)
const extendOpen = ref(false)
const moreOpen = ref(false)
const {
  detail, mutation, tags, message, revealPending, editing, editBody, editFormat, selectedTags,
  error, forwardRecipient, forwardSearch, forwardOpen, recipientUsers, notice, attachmentInput, detailUploads, detailUploadsReady,
  restorableUploads, attachmentMutationPending, viewerId, currentSensitiveBody,
  clearRevealedBody, reveal, copy, run, forward, startEdit, saveBody, removeForever,
  openViewer, closeViewer, selectViewer, chooseDetailFiles, addAttachments, addRestored, removeAttachment,
} = useMessageDetailController(() => props.id)

watch(() => props.id, () => {
  tagEditing.value = false
  extendOpen.value = false
  moreOpen.value = false
  forwardOpen.value = false
})

function onKey(event: KeyboardEvent) {
  if (event.key !== 'Escape' || viewerId.value) return
  if (forwardOpen.value || tagEditing.value || extendOpen.value || moreOpen.value) {
    forwardOpen.value = false
    tagEditing.value = false
    extendOpen.value = false
    moreOpen.value = false
    return
  }
  if (document.querySelector('.viewer, .backdrop, .queue-backdrop, .more-backdrop')) return
  emit('close')
}
onMounted(() => document.addEventListener('keydown', onKey))
onUnmounted(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="message-inspector">
    <header class="detail-header">
      <div>
        <h1 id="detail-title">
          内容详情
        </h1>
        <p
          v-if="message"
          class="muted"
        >
          {{ new Date(message.createdAt).toLocaleString() }} · v{{ message.version }}
        </p>
      </div>
      <button
        class="icon-button close"
        type="button"
        aria-label="关闭详情"
        title="关闭详情"
        @click="emit('close')"
      >
        <X aria-hidden="true" />
      </button>
    </header>
    <div
      v-if="detail.isPending.value"
      class="state"
    >
      正在加载…
    </div>
    <div
      v-else-if="detail.isError.value"
      class="state error"
    >
      详情加载失败。<button
        class="button"
        @click="detail.refetch()"
      >
        重试
      </button>
    </div>

    <template v-else-if="message">
      <section
        class="body-section"
        aria-label="正文"
      >
        <div
          v-if="message.sensitive"
          class="sensitive panel"
        >
          <template v-if="currentSensitiveBody === null">
            <strong class="sensitive-title"><LockKeyhole aria-hidden="true" />敏感内容已锁定</strong>
            <button
              class="button primary"
              :disabled="revealPending"
              @click="reveal"
            >
              {{ revealPending ? '读取中…' : '显示正文' }}
            </button>
          </template>
          <template v-else>
            <pre>{{ currentSensitiveBody }}</pre><button
              class="button"
              @click="clearRevealedBody"
            >
              隐藏
            </button>
          </template>
        </div>
        <SafeMarkdown
          v-else-if="!editing && message.bodyFormat === BodyFormat.MARKDOWN"
          :source="message.body ?? ''"
        />
        <pre
          v-else-if="!editing"
          :class="{ code: message.detectedType === 'CODE' || Boolean(message.detectedLanguage) }"
        >{{ message.body }}</pre>
        <form
          v-if="editing"
          class="edit"
          @submit.prevent="saveBody"
        >
          <label class="field">格式<select
            v-model="editFormat"
            :disabled="message.sensitive"
          ><option :value="BodyFormat.TEXT">纯文本</option><option :value="BodyFormat.MARKDOWN">Markdown</option></select></label>
          <label class="field">正文<textarea
            v-model="editBody"
            rows="12"
            required
          /></label>
          <div>
            <button
              class="button"
              type="button"
              @click="editing = false"
            >
              取消
            </button><button class="button primary">
              保存
            </button>
          </div>
        </form>
      </section>

      <section
        class="compact-section tag-section"
        aria-label="标签"
      >
        <div class="section-heading">
          <div class="tags">
            <TagChip
              v-for="tag in message.tags"
              :key="tag.id"
              :name="tag.name"
              :color="tag.color"
            /><span
              v-if="!message.tags.length"
              class="empty-inline"
            >无标签</span>
          </div>
          <button
            v-if="!message.trashedAt"
            class="icon-button"
            type="button"
            aria-label="编辑标签"
            title="编辑标签"
            :aria-expanded="tagEditing"
            @click="tagEditing = !tagEditing"
          >
            <Pencil aria-hidden="true" />
          </button>
        </div>
        <div
          v-if="tagEditing"
          class="compact-popover tag-editor"
        >
          <div class="tag-picker">
            <label
              v-for="tag in tags.data.value"
              :key="tag.id"
            ><input
              v-model="selectedTags"
              type="checkbox"
              :value="tag.id"
            > {{ tag.name }}</label><span
              v-if="!tags.data.value?.length"
              class="empty-inline"
            >还没有可用标签</span>
          </div>
          <button
            class="button small primary"
            type="button"
            @click="run({ type: 'tags', message, tagIds: selectedTags }, () => { tagEditing = false })"
          >
            完成
          </button>
        </div>
      </section>

      <section
        class="compact-section files"
        aria-label="附件"
      >
        <div class="section-heading">
          <h2>附件 ({{ message.attachments.length }})</h2>
          <button
            v-if="!message.trashedAt"
            class="icon-button"
            type="button"
            aria-label="添加附件"
            title="添加附件"
            @click="attachmentInput?.click()"
          >
            <Paperclip aria-hidden="true" />
          </button>
          <input
            ref="attachmentInput"
            class="sr-only"
            type="file"
            multiple
            @change="chooseDetailFiles"
          >
        </div>
        <AttachmentList
          v-if="message.attachments.length"
          :files="message.attachments"
          interactive
          :removable="!message.trashedAt"
          @view="openViewer"
          @remove="removeAttachment"
        />
        <p
          v-else
          class="empty-inline"
        >
          暂无附件
        </p>
        <div
          v-if="!message.trashedAt && detailUploads.length"
          class="add-files compact-popover"
          aria-label="附件上传状态"
        >
          <div
            v-for="item in detailUploads"
            :key="item.clientId"
            class="upload-row"
          >
            <span>{{ item.filename }}</span><small>{{ item.status === 'COMPLETED' ? '已就绪' : `上传中 ${Math.round(item.progress * 100)}%` }}</small>
          </div>
          <button
            class="button small primary"
            type="button"
            :disabled="!detailUploadsReady || attachmentMutationPending"
            @click="addAttachments"
          >
            {{ attachmentMutationPending ? '添加中…' : '添加' }}
          </button>
        </div>
        <details
          v-if="!message.trashedAt && restorableUploads.length"
          class="restored compact-popover"
        >
          <summary>已完成的上传 ({{ restorableUploads.length }})</summary>
          <ul>
            <li
              v-for="item in restorableUploads"
              :key="item.clientId"
            >
              <span>{{ item.filename }} · {{ formatBytes(item.size) }}</span><button
                class="button small"
                type="button"
                @click="addRestored(item.clientId)"
              >
                选用
              </button>
            </li>
          </ul>
        </details>
      </section>

      <section
        v-if="!message.trashedAt"
        class="quick-actions"
        aria-label="快捷操作"
      >
        <button
          class="button compact-action"
          type="button"
          :aria-expanded="forwardOpen"
          @click="forwardOpen = !forwardOpen"
        >
          <Forward aria-hidden="true" />转发
        </button>
        <div
          v-if="forwardOpen"
          class="forward compact-popover"
        >
          <label
            class="sr-only"
            for="forward-search"
          >搜索用户</label><input
            id="forward-search"
            v-model="forwardSearch"
            class="input"
            maxlength="100"
            placeholder="搜索用户…"
            autocomplete="off"
          >
          <p
            v-if="recipientUsers.isPending.value"
            class="muted"
          >
            正在加载用户…
          </p><p
            v-else-if="recipientUsers.isError.value"
            class="error"
          >
            用户列表加载失败，请重试。
          </p>
          <div
            v-else-if="recipientUsers.data.value?.items.length"
            class="recipient-list"
            role="listbox"
            aria-label="转发接收人"
          >
            <button
              v-for="user in recipientUsers.data.value.items"
              :key="user.id"
              class="recipient-option"
              :class="{ selected: forwardRecipient === user.id }"
              type="button"
              role="option"
              :aria-selected="forwardRecipient === user.id"
              :aria-label="`选择 ${user.displayName} @${user.username}`"
              @click="forwardRecipient = user.id"
            >
              <strong>{{ user.displayName }}</strong><span>@{{ user.username }}</span>
            </button>
          </div>
          <p
            v-else
            class="muted"
          >
            没有匹配的用户。
          </p><button
            class="button small primary forward-submit"
            type="button"
            :disabled="!forwardRecipient || mutation.isPending.value"
            @click="forward"
          >
            转发
          </button>
        </div>
      </section>

      <div class="actions">
        <template v-if="message.trashedAt">
          <button
            class="button"
            @click="run({ type: 'restore', message })"
          >
            恢复
          </button><button
            class="button danger"
            @click="removeForever(() => emit('close'))"
          >
            永久删除
          </button>
        </template>
        <template v-else>
          <button
            class="button action-button"
            type="button"
            title="复制正文"
            @click="copy"
          >
            <Copy aria-hidden="true" />复制
          </button>
          <button
            class="button action-button"
            type="button"
            title="编辑正文"
            @click="startEdit"
          >
            <Pencil aria-hidden="true" />编辑
          </button>
          <div
            v-if="message.lifecycle === 'TEMPORARY'"
            class="menu-anchor"
          >
            <button
              class="button action-button"
              type="button"
              title="延长有效期"
              :aria-expanded="extendOpen"
              @click="extendOpen = !extendOpen"
            >
              <CalendarPlus aria-hidden="true" />延长
            </button>
            <div
              v-if="extendOpen"
              class="action-menu extend-menu"
            >
              <button
                v-for="days in ([1, 3, 7] as const)"
                :key="days"
                type="button"
                @click="run({ type: 'extend', message, days }, () => { extendOpen = false })"
              >
                +{{ days }} 天
              </button>
            </div>
          </div>
          <button
            v-if="message.lifecycle === 'TEMPORARY'"
            class="button action-button"
            type="button"
            title="转为长期"
            @click="run({ type: 'permanent', message })"
          >
            <Archive aria-hidden="true" />长期
          </button>
          <button
            v-else
            class="button action-button"
            type="button"
            :title="message.favorite ? '取消收藏' : '收藏'"
            @click="run({ type: 'favorite', message, favorite: !message.favorite })"
          >
            <Star aria-hidden="true" />{{ message.favorite ? '已收藏' : '收藏' }}
          </button>
          <div class="menu-anchor">
            <button
              class="icon-button"
              type="button"
              aria-label="更多操作"
              title="更多操作"
              :aria-expanded="moreOpen"
              @click="moreOpen = !moreOpen"
            >
              <MoreHorizontal aria-hidden="true" />
            </button>
            <div
              v-if="moreOpen"
              class="action-menu more-menu"
            >
              <button
                type="button"
                @click="run({ type: 'sensitive', message, sensitive: !message.sensitive }, () => { moreOpen = false })"
              >
                {{ message.sensitive ? '取消敏感' : '设为敏感' }}
              </button><button
                class="danger-item"
                type="button"
                @click="run({ type: 'trash', message }, () => { moreOpen = false })"
              >
                <Trash2 aria-hidden="true" />删除
              </button>
            </div>
          </div>
        </template>
      </div>
      <p
        v-if="notice"
        class="notice"
        role="status"
      >
        {{ notice }}
      </p><p
        v-if="error"
        class="error"
        role="alert"
      >
        {{ error }}
      </p>
    </template>
    <AttachmentViewer
      v-if="message && viewerId"
      :files="message.attachments"
      :current-id="viewerId"
      @close="closeViewer"
      @select="selectViewer"
    />
  </div>
</template>

<style scoped>
.message-inspector{display:grid;align-content:start;gap:0;min-height:100%;padding:1.15rem 1.2rem 5.25rem;color:var(--text-primary)}.detail-header{display:flex;justify-content:space-between;gap:1rem;align-items:flex-start;position:sticky;top:-1.15rem;z-index:5;margin:-1.15rem -1.2rem 0;padding:1.15rem 1.2rem .9rem;background:color-mix(in srgb,var(--surface-raised) 94%,transparent);backdrop-filter:blur(12px);border-bottom:1px solid var(--border-default)}h1,h2,p{margin:.2rem 0}h1{font-size:1.12rem}h2{font-size:.78rem}.detail-header p{font-size:.7rem}.icon-button{display:inline-grid;place-items:center;flex:0 0 auto;width:34px;height:34px;padding:0;border:0;border-radius:9px;background:transparent;color:var(--text-secondary)}.icon-button:hover{background:var(--surface-soft);color:var(--text-primary)}.icon-button svg,.action-button svg,.compact-action svg,.sensitive-title svg{width:1rem;height:1rem}
.body-section{padding:1.15rem 0 1.25rem}.body-section pre{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.65}.code{font-family:var(--font-mono);background:var(--surface-soft);padding:.9rem;border-radius:var(--radius)}.sensitive{display:grid;gap:.75rem;padding:1rem;box-shadow:none}.sensitive-title{display:inline-flex;align-items:center;gap:.4rem}.sensitive .button{justify-self:start}.edit{display:grid;gap:.7rem}.edit textarea{resize:vertical}.edit>div{display:flex;gap:.45rem}
.compact-section{position:relative;padding:.8rem 0;border-top:1px solid var(--border-default)}.section-heading{display:flex;align-items:center;justify-content:space-between;gap:.75rem;min-height:34px}.tags{display:flex;flex-wrap:wrap;gap:.35rem}.empty-inline{color:var(--text-tertiary);font-size:.75rem}.compact-popover{margin-top:.55rem;padding:.65rem;border:1px solid var(--border-default);border-radius:var(--radius-sm);background:var(--surface-soft)}.tag-editor{display:grid;gap:.55rem}.tag-picker{display:flex;flex-wrap:wrap;gap:.4rem}.tag-picker label{padding:.25rem .4rem;border-radius:.4rem;background:var(--surface-raised);font-size:.76rem}.tag-editor .button{justify-self:end}
.files{display:grid}.files :deep(.attachment-list){margin-top:.45rem}.files :deep(.attachment){grid-template-columns:34px minmax(0,1fr);padding:.4rem;border:0;background:transparent}.files :deep(.attachment img),.files :deep(.attachment .file-icon){width:34px;height:32px}.files :deep(.remove){min-height:30px;padding:.25rem .4rem;font-size:.7rem}.add-files{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:.35rem .6rem}.upload-row{grid-column:1;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:.5rem;font-size:.76rem}.upload-row span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.upload-row small{color:var(--text-tertiary)}.add-files>.button{grid-column:2;grid-row:1/-1;align-self:center}.restored summary{cursor:pointer;font-size:.76rem;font-weight:650}.restored ul{list-style:none;margin:.55rem 0 0;padding:0;display:grid;gap:.35rem}.restored li{display:flex;align-items:center;justify-content:space-between;gap:.6rem;font-size:.74rem}.restored li span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.quick-actions{position:relative;padding:.8rem 0;border-top:1px solid var(--border-default)}.compact-action,.action-button{display:inline-flex;align-items:center;gap:.32rem;min-height:34px;padding:.35rem .55rem;font-size:.76rem}.forward{display:grid;gap:.45rem}.recipient-list{display:grid;gap:.25rem;max-height:220px;overflow:auto}.recipient-option{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:.5rem;width:100%;padding:.45rem .5rem;border:1px solid transparent;border-radius:var(--radius-sm);background:var(--surface-raised);color:var(--text-primary);text-align:left}.recipient-option.selected{border-color:var(--accent-primary);background:var(--accent-primary-soft)}.recipient-option strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.78rem}.recipient-option span{color:var(--text-tertiary);font-size:.7rem}.forward-submit{justify-self:end}
.actions{position:sticky;z-index:4;bottom:-1.15rem;display:flex;align-items:center;gap:.3rem;margin:0 -1.2rem -5.25rem;padding:.75rem 1.2rem calc(.75rem + env(safe-area-inset-bottom));border-top:1px solid var(--border-default);background:color-mix(in srgb,var(--surface-raised) 96%,transparent);backdrop-filter:blur(12px)}.menu-anchor{position:relative}.action-menu{position:absolute;z-index:10;bottom:calc(100% + .45rem);min-width:118px;padding:.3rem;border:1px solid var(--border-default);border-radius:var(--radius-sm);background:var(--surface-raised);box-shadow:var(--shadow-floating)}.extend-menu{left:0}.more-menu{right:0}.action-menu button{display:flex;align-items:center;gap:.4rem;width:100%;min-height:34px;padding:.4rem .55rem;border:0;border-radius:.4rem;background:transparent;color:var(--text-primary);font-size:.76rem;text-align:left}.action-menu button:hover{background:var(--surface-soft)}.action-menu svg{width:.9rem}.action-menu .danger-item{color:var(--state-danger)}.notice{margin:.65rem 0;color:var(--accent-primary);font-size:.76rem}.state{padding:3rem 1rem;text-align:center}.small{min-height:32px;padding:.3rem .55rem;font-size:.74rem}
@media(max-width:600px){.message-inspector{padding:1rem 1rem 5.5rem}.detail-header{top:-1rem;margin:-1rem -1rem 0;padding:1rem}.actions{bottom:-1rem;margin:0 -1rem -5.5rem;padding-inline:1rem;overflow-x:auto}.action-button{white-space:nowrap}.recipient-option{grid-template-columns:minmax(0,1fr)}.add-files{grid-template-columns:1fr}.add-files>.button{grid-column:1;grid-row:auto;justify-self:end}}
</style>
