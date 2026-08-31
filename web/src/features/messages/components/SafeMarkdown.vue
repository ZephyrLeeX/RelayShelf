<script setup lang="ts">
/* eslint-disable vue/no-v-html -- this dedicated boundary sanitizes renderer output with DOMPurify */
import { onBeforeUnmount, ref, watch } from 'vue'
import DOMPurify from 'dompurify'
import MarkdownIt from 'markdown-it'
import hljs from 'highlight.js/lib/core'

const props = defineProps<{ source: string }>()
const rendered = ref('')
let generation = 0
const languageLoaders: Record<string, () => Promise<{ default: Parameters<typeof hljs.registerLanguage>[1] }>> = {
  shell: () => import('highlight.js/lib/languages/shell'), bash: () => import('highlight.js/lib/languages/bash'),
  sh: () => import('highlight.js/lib/languages/bash'), zsh: () => import('highlight.js/lib/languages/bash'),
  console: () => import('highlight.js/lib/languages/shell'),
  json: () => import('highlight.js/lib/languages/json'), yaml: () => import('highlight.js/lib/languages/yaml'),
  yml: () => import('highlight.js/lib/languages/yaml'),
  javascript: () => import('highlight.js/lib/languages/javascript'), js: () => import('highlight.js/lib/languages/javascript'),
  typescript: () => import('highlight.js/lib/languages/typescript'), ts: () => import('highlight.js/lib/languages/typescript'),
  python: () => import('highlight.js/lib/languages/python'), py: () => import('highlight.js/lib/languages/python'),
  sql: () => import('highlight.js/lib/languages/sql'),
  markdown: () => import('highlight.js/lib/languages/markdown'), md: () => import('highlight.js/lib/languages/markdown'),
  java: () => import('highlight.js/lib/languages/java'),
  go: () => import('highlight.js/lib/languages/go'), golang: () => import('highlight.js/lib/languages/go'),
  rust: () => import('highlight.js/lib/languages/rust'), rs: () => import('highlight.js/lib/languages/rust'),
  c: () => import('highlight.js/lib/languages/c'),
  cpp: () => import('highlight.js/lib/languages/cpp'), cxx: () => import('highlight.js/lib/languages/cpp'),
  csharp: () => import('highlight.js/lib/languages/csharp'), cs: () => import('highlight.js/lib/languages/csharp'),
  powershell: () => import('highlight.js/lib/languages/powershell'), ps1: () => import('highlight.js/lib/languages/powershell'),
  dockerfile: () => import('highlight.js/lib/languages/dockerfile'), docker: () => import('highlight.js/lib/languages/dockerfile'),
  html: () => import('highlight.js/lib/languages/xml'), xml: () => import('highlight.js/lib/languages/xml'),
  css: () => import('highlight.js/lib/languages/css'),
}
const loaded = new Set<string>()

async function render(source: string) {
  const current = ++generation
  const languages = [...source.matchAll(/```([\w-]+)/g)].map((match) => match[1].toLowerCase()).filter((name) => name in languageLoaders && !loaded.has(name))
  await Promise.all([...new Set(languages)].map(async (name) => { const module = await languageLoaders[name](); hljs.registerLanguage(name, module.default); loaded.add(name) }))
  if (current !== generation) return
  const markdown = new MarkdownIt({ html: false, linkify: true })
  markdown.options.highlight = (code: string, language: string) => {
    if (language && hljs.getLanguage(language)) return `<pre class="hljs"><code>${hljs.highlight(code, { language }).value}</code></pre>`
    return `<pre class="hljs"><code>${markdown.utils.escapeHtml(code)}</code></pre>`
  }
  markdown.validateLink = (url: string) => /^(https?):/i.test(url)
  type RenderRule = NonNullable<typeof markdown.renderer.rules.link_open>
  const imageRule: RenderRule = (tokens, index) => {
    const href = String(tokens[index].attrGet('src') ?? '')
    return markdown.validateLink(href) ? `<a href="${markdown.utils.escapeHtml(href)}" rel="noopener noreferrer" target="_blank">[远程图片链接]</a>` : '[图片已阻止]'
  }
  markdown.renderer.rules.image = imageRule
  const fallbackLink: RenderRule = (tokens, index, options, _env, self) => self.renderToken(tokens, index, options)
  const originalLink = markdown.renderer.rules.link_open ?? fallbackLink
  const linkRule: RenderRule = (tokens, index, options, env, self) => {
    tokens[index].attrSet('target', '_blank'); tokens[index].attrSet('rel', 'noopener noreferrer')
    return originalLink(tokens, index, options, env, self)
  }
  markdown.renderer.rules.link_open = linkRule
  rendered.value = DOMPurify.sanitize(markdown.render(source), {
    ALLOWED_TAGS: ['p', 'br', 'em', 'strong', 's', 'blockquote', 'ul', 'ol', 'li', 'pre', 'code', 'a', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr'],
    ALLOWED_ATTR: ['href', 'title', 'target', 'rel', 'class'],
    ALLOW_UNKNOWN_PROTOCOLS: false,
  })
}
watch(() => props.source, (value) => { void render(value) }, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>

<template>
  <!-- Renderer output is sanitized by DOMPurify with a narrow tag/attribute allowlist above. -->
  <div
    class="safe-markdown"
    v-html="rendered"
  />
</template>

<style scoped>
.safe-markdown{line-height:1.65;overflow-wrap:anywhere}.safe-markdown :deep(pre){overflow:auto;padding:1rem;border-radius:var(--radius-sm);background:#17211e;color:#e7f0ec;font-family:var(--font-mono);font-size:.86rem}.safe-markdown :deep(code:not(pre code)){padding:.12rem .3rem;border-radius:4px;background:var(--surface-soft);font-family:var(--font-mono)}.safe-markdown :deep(a){color:var(--accent-strong)}.safe-markdown :deep(blockquote){margin-left:0;padding-left:1rem;border-left:3px solid var(--accent);color:var(--muted)}
</style>
