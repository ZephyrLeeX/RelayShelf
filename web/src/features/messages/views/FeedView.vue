<script setup lang="ts">
import { computed } from 'vue'
import { Lifecycle } from '@/api/generated'
import MessageComposer from '../components/MessageComposer.vue'
import MessageFeed from '../components/MessageFeed.vue'
import { useTagsQuery } from '@/features/tags/queries'

type FeedKind = 'temporary' | 'permanent' | 'favorites' | 'tag' | 'trash'
const props = defineProps<{ kind: FeedKind; tagId?: string }>()
const tags = useTagsQuery()
const title = computed(() => ({ temporary:'临时区', permanent:'长期区', favorites:'收藏', tag: tags.data.value?.find((tag) => tag.id === props.tagId)?.name || '标签', trash:'回收站' })[props.kind])
const description = computed(() => ({ temporary:'快速放下，按到期时间自然整理。', permanent:'长期保留的文字与知识片段。', favorites:'你标记为重要的长期内容。', tag:'按标签聚合的内容。', trash:'可恢复，或永久删除。' })[props.kind])
const emptyText = computed(() => ({ temporary:'暂无临时内容', permanent:'暂无长期内容', favorites:'还没有收藏', tag:'该标签暂无内容', trash:'回收站为空' })[props.kind])
const filters = computed(() => {
  if (props.kind === 'temporary') return { lifecycle: Lifecycle.TEMPORARY }
  if (props.kind === 'permanent') return { lifecycle: Lifecycle.PERMANENT }
  if (props.kind === 'favorites') return { favorite: true }
  if (props.kind === 'tag') return { tagIds: props.tagId ? [props.tagId] : [] }
  return { trash: true }
})
</script>

<template>
  <section class="page">
    <header>
      <h1>{{ title }}</h1><p class="muted">
        {{ description }}
      </p>
    </header>
    <div
      v-if="kind === 'temporary' || kind === 'permanent'"
      class="composer-shell"
    >
      <MessageComposer :default-lifecycle="kind === 'temporary' ? Lifecycle.TEMPORARY : Lifecycle.PERMANENT" />
    </div>
    <MessageFeed
      :filters="filters"
      :empty-text="emptyText"
    />
  </section>
</template>

<style scoped>
.page{display:grid;gap:1rem}.page>header h1,.page>header p{margin:.2rem 0}.page>header h1{font-size:1.65rem}
.composer-shell{width:min(84%,760px);margin-inline:auto}
@media(max-width:1179px){.composer-shell{width:min(100%,760px)}}
</style>
