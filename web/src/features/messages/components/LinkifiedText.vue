<script setup lang="ts">
import { computed } from 'vue'

/**
 * Plain-text renderer that turns bare URLs into safe anchors. Text is split
 * into Vue text nodes and <a> elements — never v-html — so plain TEXT bodies
 * keep their whitespace semantics while http(s)/www links become clickable.
 * Only http/https survive normalization; javascript:, data:, file: and other
 * schemes can never match the URL pattern in the first place.
 */
const props = defineProps<{ text: string }>()

interface Segment {
  kind: 'text' | 'link'
  text: string
  href?: string
}

// URL candidates are printable ASCII only: CJK prose (including 。；） and
// friends) terminates the URL instead of being swallowed into the href.
const URL_PATTERN = /\b(?:https?:\/\/|www\.)[^\s<>"'\u0080-\uffff]+/gi
const TRAILING_PUNCTUATION = new Set([
  '.', ',', ';', ':', '!', '?', ')', ']', '}', '>', '\'', '"', '…',
  '。', '，', '、', '；', '：', '！', '？', '）', '】', '》', '」', '』',
])

/** Trailing prose punctuation stays out of the href; balanced ')' is kept. */
function trimTrailingUrl(raw: string): string {
  let url = raw
  while (url.length > 0) {
    const last = url[url.length - 1]
    if (last === undefined || !TRAILING_PUNCTUATION.has(last)) break
    if (last === ')') {
      const opens = (url.match(/\(/g) ?? []).length
      const closes = (url.match(/\)/g) ?? []).length
      if (opens >= closes) break
    }
    url = url.slice(0, -1)
  }
  return url
}

function toHref(candidate: string): string | null {
  const trimmed = trimTrailingUrl(candidate)
  if (!trimmed) return null
  const href = /^www\./i.test(trimmed) ? `https://${trimmed}` : trimmed
  return /^https?:\/\/(?:localhost(?![\w.])|[^\s/#?]+\.[^\s/#?]+)/i.test(href) ? href : null
}

const segments = computed<Segment[]>(() => {
  const result: Segment[] = []
  let lastIndex = 0
  for (const match of props.text.matchAll(URL_PATTERN)) {
    const raw = match[0]
    const trimmed = trimTrailingUrl(raw)
    const href = toHref(raw)
    if (!trimmed || !href) continue
    const index = match.index ?? 0
    if (index > lastIndex) result.push({ kind: 'text', text: props.text.slice(lastIndex, index) })
    result.push({ kind: 'link', text: trimmed, href })
    lastIndex = index + raw.length
  }
  if (lastIndex < props.text.length) result.push({ kind: 'text', text: props.text.slice(lastIndex) })
  return result
})
</script>

<template>
  <span class="linkified-text">
    <template
      v-for="(segment, index) in segments"
      :key="index"
    >
      <a
        v-if="segment.kind === 'link'"
        :href="segment.href"
        target="_blank"
        rel="noopener noreferrer"
        @click.stop
      >{{ segment.text }}</a>
      <template v-else>{{ segment.text }}</template>
    </template>
  </span>
</template>

<style scoped>
.linkified-text{white-space:pre-wrap;overflow-wrap:anywhere}
a{color:var(--accent-primary);text-decoration:underline;text-underline-offset:2px}
a:hover{color:var(--accent-primary-hover)}
a:focus-visible{outline:2px solid var(--focus-ring);outline-offset:1px;border-radius:2px}
</style>
