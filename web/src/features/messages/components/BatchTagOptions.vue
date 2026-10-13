<script setup lang="ts">
import { useTagsQuery } from '@/features/tags/queries'
const selected = defineModel<string[]>({ required: true })
defineProps<{ disabled: boolean }>()
const tags = useTagsQuery()
</script>

<template>
  <fieldset
    class="batch-tags"
    :disabled="disabled"
  >
    <legend>选择要追加的标签（保留原标签）</legend>
    <p v-if="tags.isPending.value">
      正在加载标签…
    </p>
    <p
      v-else-if="tags.isError.value"
      role="alert"
    >
      标签加载失败 <button
        type="button"
        class="button"
        @click="tags.refetch()"
      >
        重试
      </button>
    </p>
    <p v-else-if="!tags.data.value?.length">
      暂无标签，请先在标签管理中新建。
    </p>
    <label
      v-for="tag in tags.data.value"
      :key="tag.id"
    >
      <input
        v-model="selected"
        type="checkbox"
        :value="tag.id"
      >{{ tag.name }}
    </label>
  </fieldset>
</template>

<style scoped>
.batch-tags{display:flex;flex-wrap:wrap;gap:.6rem;border:1px solid var(--border-default);border-radius:var(--radius-sm);max-height:180px;overflow:auto}.batch-tags label{display:flex;align-items:center;gap:.35rem;min-height:36px}.batch-tags p{margin:0}.batch-tags legend{font-size:.8rem}
</style>
