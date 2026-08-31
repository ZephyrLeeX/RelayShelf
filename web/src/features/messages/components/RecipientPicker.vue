<script setup lang="ts">
import { ChevronDown, Search, User } from '@lucide/vue'
import { computed, ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { DefaultService, type RecipientUser } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { useDismiss } from '@/shared/composables/useDismiss'

/**
 * Shared recipient selector for Composer direct send and detail forwarding.
 * Users are picked by display name / username search — raw user IDs never
 * appear in the UI; `null` means "send to myself".
 */
const recipient = defineModel<RecipientUser | null>({ required: true })
// "bottom" suits the Composer (menu drops over the textarea); "top" is for
// popovers near the bottom of a scrolling sheet so the menu is not clipped.
withDefaults(defineProps<{ placement?: 'bottom' | 'top' }>(), { placement: 'bottom' })

const open = ref(false)
const root = ref<HTMLElement>()
const search = ref('')
useDismiss(open, root)

const results = useQuery({
  queryKey: computed(() => queryKeys.recipients.list(search.value.trim())),
  queryFn: ({ queryKey }) => DefaultService.listRecipientUsers(queryKey[1] || undefined, 8),
  enabled: open,
})

function choose(user: RecipientUser | null) {
  recipient.value = user
  open.value = false
  search.value = ''
}
</script>

<template>
  <div
    ref="root"
    class="recipient-picker"
  >
    <button
      class="recipient-trigger"
      type="button"
      :class="{ active: recipient !== null }"
      :aria-expanded="open"
      aria-haspopup="listbox"
      aria-label="接收人"
      :title="recipient ? `直发给 @${recipient.username}` : '接收人：自己'"
      @click="open = !open"
    >
      <User
        aria-hidden="true"
        class="user-icon"
      />
      <span class="recipient-name">{{ recipient ? `@${recipient.username}` : '自己' }}</span>
      <ChevronDown
        class="chevron"
        aria-hidden="true"
      />
    </button>
    <div
      v-if="open"
      class="recipient-menu"
      :class="{ 'placement-top': placement === 'top' }"
      role="listbox"
      aria-label="接收人"
    >
      <div class="search-box">
        <Search
          aria-hidden="true"
          class="search-icon"
        />
        <label class="sr-only">
          搜索用户
        </label>
        <input
          v-model="search"
          type="search"
          maxlength="100"
          placeholder="搜索用户…"
          autocomplete="off"
        >
      </div>
      <button
        type="button"
        role="option"
        class="recipient-option"
        :aria-selected="recipient === null"
        :class="{ selected: recipient === null }"
        title="发送给自己"
        @click="choose(null)"
      >
        <User
          aria-hidden="true"
          class="option-icon"
        /><strong>自己</strong><span>普通发送</span>
      </button>
      <p
        v-if="results.isPending.value"
        class="hint"
      >
        正在加载用户…
      </p>
      <p
        v-else-if="results.isError.value"
        class="hint error"
      >
        用户列表加载失败，请重试。
      </p>
      <template v-else-if="results.data.value?.items.length">
        <button
          v-for="user in results.data.value.items"
          :key="user.id"
          type="button"
          role="option"
          class="recipient-option"
          :aria-selected="recipient?.id === user.id"
          :class="{ selected: recipient?.id === user.id }"
          :aria-label="`选择 ${user.displayName} @${user.username}`"
          @click="choose(user)"
        >
          <strong>{{ user.displayName }}</strong><span>@{{ user.username }}</span>
        </button>
      </template>
      <p
        v-else
        class="hint"
      >
        没有匹配的用户。
      </p>
    </div>
  </div>
</template>

<style scoped>
.recipient-picker{position:relative}.recipient-trigger{display:inline-flex;align-items:center;gap:.35rem;min-height:34px;border:1px solid var(--border-default);border-radius:.6rem;padding:.28rem .55rem;background:var(--surface-raised);color:var(--text-primary);font-size:.78rem;font-weight:650;cursor:pointer}.recipient-trigger:hover{border-color:var(--border-strong);background:var(--surface-soft)}.recipient-trigger.active{border-color:var(--accent-secondary);color:var(--accent-secondary);background:color-mix(in srgb,var(--accent-secondary) 10%,var(--surface-raised))}.recipient-trigger:focus-visible{outline:2px solid var(--focus-ring);outline-offset:2px}
.user-icon,.option-icon{width:.95rem;height:.95rem}.recipient-name{max-width:8.5rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.chevron{width:.85rem;height:.85rem;color:var(--text-tertiary)}
.recipient-menu{position:absolute;z-index:30;top:calc(100% + .4rem);right:0;display:grid;gap:.3rem;width:min(280px,calc(100vw - 2rem));padding:.5rem;border:1px solid var(--border-default);border-radius:var(--radius-sm);background:var(--surface-raised);box-shadow:var(--shadow-floating)}/* In top placement (detail popovers) the trigger sits near the sheet's left
   edge, so anchoring right would push the 280px menu past the clipped sheet
   boundary. Anchor left instead. */
.recipient-menu.placement-top{top:auto;bottom:calc(100% + .4rem);left:0;right:auto}
.search-box{display:flex;align-items:center;gap:.4rem;padding:.3rem .45rem;border:1px solid var(--border-default);border-radius:.45rem;background:var(--surface-soft)}.search-box:focus-within{border-color:var(--accent-primary)}.search-icon{width:.85rem;height:.85rem;color:var(--text-tertiary)}.search-box input{width:100%;border:0;background:transparent;color:var(--text-primary);font-size:.78rem;outline:0}.search-box input::placeholder{color:var(--text-tertiary)}
.recipient-option{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.45rem;width:100%;min-height:38px;border:1px solid transparent;border-radius:.5rem;padding:.35rem .5rem;background:transparent;color:var(--text-primary);text-align:left;cursor:pointer}.recipient-option:hover{background:var(--surface-soft)}.recipient-option.selected{border-color:var(--accent-secondary);background:color-mix(in srgb,var(--accent-secondary) 12%,var(--surface-raised))}.recipient-option:focus-visible{outline:2px solid var(--focus-ring);outline-offset:-2px}.recipient-option strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.78rem}.recipient-option span{color:var(--text-tertiary);font-size:.7rem}
.hint{margin:.2rem .3rem;color:var(--text-tertiary);font-size:.74rem}.hint.error{color:var(--state-danger)}
@media(max-width:480px){.recipient-trigger .chevron{display:none}.recipient-trigger{padding-inline:.45rem}}
</style>
