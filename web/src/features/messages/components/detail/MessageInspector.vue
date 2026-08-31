<script setup lang="ts">
import { Copy, LockKeyhole, Pencil, Star, Trash2, X } from '@lucide/vue'
import { defineAsyncComponent, onMounted, onUnmounted } from 'vue'
import { BodyFormat } from '@/api/generated'
import { formatBytes } from '@/shared/utils/bytes'
import TagChip from '@/shared/ui/TagChip.vue'
import AttachmentList from '../AttachmentList.vue'
import SafeMarkdown from '../SafeMarkdown.vue'
import { useMessageDetailController } from '../../composables/useMessageDetailController'

const AttachmentViewer = defineAsyncComponent(() => import('@/features/files/AttachmentViewer.vue'))
const props = defineProps<{ id: string }>()
const emit = defineEmits<{ close: [] }>()
const {
  detail, mutation, tags, message, revealPending, editing, editBody, editFormat, selectedTags,
  error, forwardRecipient, forwardSearch, recipientUsers, notice, attachmentInput, detailUploads, detailUploadsReady,
  restorableUploads, attachmentMutationPending, viewerId, currentSensitiveBody,
  clearRevealedBody, reveal, copy, run, forward, startEdit, saveBody, removeForever,
  openViewer, closeViewer, selectViewer, chooseDetailFiles, addAttachments, addRestored,
  removeAttachment,
} = useMessageDetailController(() => props.id)

function onKey(event: KeyboardEvent) {
  if (event.key !== 'Escape' || viewerId.value) return
  // A shell-level modal opened over the inspector owns the first Escape.
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
        class="button close"
        aria-label="关闭详情"
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
        v-if="message.sensitive"
        class="sensitive panel"
      >
        <template v-if="currentSensitiveBody === null">
          <strong class="sensitive-title"><LockKeyhole aria-hidden="true" />敏感内容已锁定</strong>
          <p class="muted">
            明文仅在本详情面板内存中短暂保留。
          </p>
          <button
            class="button primary"
            :disabled="revealPending"
            @click="reveal"
          >
            {{ revealPending ? '读取中…' : '显示正文' }}
          </button>
        </template>
        <template v-else>
          <pre>{{ currentSensitiveBody }}</pre>
          <button
            class="button"
            @click="clearRevealedBody"
          >
            隐藏
          </button>
        </template>
      </section>
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
      <div class="tags">
        <TagChip
          v-for="tag in message.tags"
          :key="tag.id"
          :name="tag.name"
          :color="tag.color"
        />
      </div>
      <details v-if="!message.trashedAt">
        <summary>修改标签</summary>
        <div class="tag-picker">
          <label
            v-for="tag in tags.data.value"
            :key="tag.id"
          ><input
            v-model="selectedTags"
            type="checkbox"
            :value="tag.id"
          > {{ tag.name }}</label>
        </div>
        <button
          class="button"
          @click="run({ type: 'tags', message, tagIds: selectedTags })"
        >
          保存标签
        </button>
      </details>
      <section
        v-if="message.attachments.length"
        class="files"
      >
        <h2>附件</h2>
        <AttachmentList
          :files="message.attachments"
          interactive
          :removable="!message.trashedAt"
          @view="openViewer"
          @remove="removeAttachment"
        />
      </section>
      <section
        v-if="!message.trashedAt"
        class="add-files panel"
      >
        <input
          ref="attachmentInput"
          class="sr-only"
          type="file"
          multiple
          @change="chooseDetailFiles"
        >
        <div><strong>添加附件</strong><span v-if="detailUploads.length">{{ detailUploads.length }} 个文件 · {{ detailUploadsReady ? '已就绪' : '上传中' }}</span></div>
        <button
          class="button"
          type="button"
          @click="attachmentInput?.click()"
        >
          选择文件
        </button>
        <button
          v-if="detailUploads.length"
          class="button primary"
          type="button"
          :disabled="!detailUploadsReady || attachmentMutationPending"
          @click="addAttachments"
        >
          {{ attachmentMutationPending ? '添加中…' : '添加到内容' }}
        </button>
      </section>
      <section
        v-if="!message.trashedAt && restorableUploads.length"
        class="restored"
        aria-label="已完成的上传"
      >
        <h2>已完成的上传</h2>
        <ul>
          <li
            v-for="item in restorableUploads"
            :key="item.clientId"
          >
            <span>{{ item.filename }} · {{ formatBytes(item.size) }}</span>
            <button
              class="button"
              type="button"
              @click="addRestored(item.clientId)"
            >
              添加到当前内容
            </button>
          </li>
        </ul>
      </section>
      <section
        v-if="!message.trashedAt"
        class="forward panel"
      >
        <strong>转发给其他用户</strong>
        <label class="field forward-search">搜索接收人<input
          v-model="forwardSearch"
          class="input"
          maxlength="100"
          placeholder="用户名或显示名称"
          autocomplete="off"
        ></label>
        <p
          v-if="recipientUsers.isPending.value"
          class="muted"
        >
          正在加载用户…
        </p>
        <p
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
          没有匹配的可转发用户。
        </p>
        <button
          class="button forward-submit"
          type="button"
          :disabled="!forwardRecipient || mutation.isPending.value"
          @click="forward"
        >
          转发副本
        </button>
        <p class="muted">
          接收者会得到一条独立的临时副本；此内容保持不变。
        </p>
      </section>
      <div class="actions">
        <template v-if="message.trashedAt">
          <button
            class="button"
            @click="run({ type: 'restore', message })"
          >
            恢复
          </button>
          <button
            class="button danger"
            @click="removeForever(() => emit('close'))"
          >
            永久删除
          </button>
        </template>
        <template v-else>
          <button
            class="button"
            @click="copy"
          >
            <Copy aria-hidden="true" />复制正文
          </button>
          <button
            class="button"
            @click="startEdit"
          >
            <Pencil aria-hidden="true" />编辑
          </button>
          <button
            class="button"
            @click="run({ type: 'sensitive', message, sensitive: !message.sensitive })"
          >
            {{ message.sensitive ? '转为普通内容' : '设为敏感内容' }}
          </button>
          <button
            v-if="message.lifecycle === 'TEMPORARY'"
            class="button"
            @click="run({ type: 'permanent', message })"
          >
            转为长期
          </button>
          <button
            v-else
            class="button"
            @click="run({ type: 'favorite', message, favorite: !message.favorite })"
          >
            <Star aria-hidden="true" />{{ message.favorite ? '取消收藏' : '收藏' }}
          </button>
          <button
            class="button danger"
            @click="run({ type: 'trash', message })"
          >
            <Trash2 aria-hidden="true" />移到回收站
          </button>
        </template>
      </div>
      <p
        v-if="notice"
        role="status"
      >
        {{ notice }}
      </p>
      <p
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
.message-inspector{display:grid;align-content:start;gap:1rem;min-height:100%;padding:1.3rem;color:var(--text-primary)}.detail-header{display:flex;justify-content:space-between;gap:1rem;align-items:flex-start;position:sticky;top:-1.3rem;z-index:2;margin:-1.3rem -1.3rem 0;padding:1.3rem;background:color-mix(in srgb,var(--surface-raised) 94%,transparent);backdrop-filter:blur(12px);border-bottom:1px solid var(--border-default)}h1,h2,p{margin:.2rem 0}h1{font-size:1.25rem}.close svg,.actions svg,.sensitive-title svg{width:1rem;height:1rem}.sensitive-title,.actions .button{display:inline-flex;align-items:center;gap:.35rem}pre{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.6}.code{font-family:var(--font-mono);background:var(--surface-soft);padding:1rem;border-radius:var(--radius)}.sensitive{padding:1.25rem;box-shadow:none}.edit{display:grid;gap:.75rem}.edit textarea{resize:vertical}.edit>div{display:flex;gap:.5rem}.tags,.actions,.tag-picker{display:flex;flex-wrap:wrap;gap:.5rem}.files{display:grid;gap:.5rem}.add-files{display:flex;align-items:center;gap:.5rem;padding:.7rem;box-shadow:none}.add-files>div{display:grid;flex:1}.add-files span{color:var(--text-tertiary);font-size:.75rem}details{display:grid;gap:.6rem}summary{cursor:pointer}.state{padding:3rem 1rem;text-align:center}.forward{display:grid;gap:.5rem;padding:.7rem;box-shadow:none}.forward-search{font-size:.76rem}.recipient-list{display:grid;gap:.35rem}.recipient-option{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:.5rem;width:100%;border:1px solid var(--border-default);border-radius:var(--radius-sm);padding:.5rem .6rem;background:var(--surface-raised);color:var(--text-primary);text-align:left}.recipient-option:hover{border-color:var(--border-strong)}.recipient-option.selected{border-color:var(--accent-primary);background:var(--accent-primary-soft)}.recipient-option strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.8rem}.recipient-option span{color:var(--text-tertiary);font-size:.72rem}.forward-submit{justify-self:end}.restored{border:1px solid var(--border-default);border-radius:var(--radius-sm);padding:.55rem .65rem;display:grid;gap:.4rem}.restored h2{font-size:.78rem;color:var(--text-tertiary)}.restored ul{list-style:none;margin:0;padding:0;display:grid;gap:.35rem}.restored li{display:flex;align-items:center;justify-content:space-between;gap:.7rem;font-size:.82rem}.restored span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.restored .button{min-height:32px;padding:.25rem .55rem;font-size:.76rem;white-space:nowrap}
@media(max-width:600px){.message-inspector{padding:1rem}.detail-header{top:-1rem;margin:-1rem -1rem 0;padding:1rem}.add-files{flex-wrap:wrap}.add-files>div{width:100%;flex-basis:100%}.recipient-option{grid-template-columns:minmax(0,1fr)}.forward-submit{justify-self:stretch}.restored li{align-items:flex-start;flex-direction:column}}
</style>
