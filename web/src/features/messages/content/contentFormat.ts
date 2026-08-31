import { BodyFormat } from '@/api/generated'
import { CODE_LANGUAGES, findCodeLanguage } from './codeLanguages'

/**
 * Unified content-format bridge between what the user edits and what the API
 * stores. The user picks a content type ("Shell", "Python", "Markdown", plain
 * text) and edits raw text; serialization wraps code types into a single
 * Markdown fenced block so the stored contract stays TEXT / MARKDOWN.
 */

export type ContentTypeId = 'text' | 'markdown' | (typeof CODE_LANGUAGES)[number]['id']

export interface ContentTypeOption {
  id: ContentTypeId
  label: string
  shortLabel: string
  kind: 'plain' | 'markdown' | 'code'
  hint: string
}

export const CONTENT_TYPES: ContentTypeOption[] = [
  { id: 'text', label: '纯文本', shortLabel: 'Aa', kind: 'plain', hint: '按原样保存，链接自动可点击' },
  { id: 'markdown', label: 'Markdown', shortLabel: 'MD', kind: 'markdown', hint: '按 Markdown 渲染' },
  ...CODE_LANGUAGES.map((language) => ({
    id: language.id,
    label: language.label,
    shortLabel: language.shortLabel,
    kind: 'code' as const,
    hint: `以 ${language.label} 代码块保存`,
  })),
]

export function findContentType(id: string): ContentTypeOption | null {
  return CONTENT_TYPES.find((type) => type.id === id) ?? null
}

export function isCodeContentType(id: string): boolean {
  return findContentType(id)?.kind === 'code'
}

export interface SerializedContent {
  body: string
  bodyFormat: BodyFormat
}

export interface ParsedContent {
  typeId: ContentTypeId
  /** Text as it should appear in an editing textarea (no Markdown fence). */
  text: string
}

export interface FencedCode {
  /** Raw fence token, e.g. "py" — resolve with findCodeLanguage. */
  language: string
  code: string
}

// Opening fence: a run of at least three backticks, an optional info
// language, then the end of the line. The length is variable because the
// serializer grows the fence when the code itself contains backtick runs.
const OPENING_FENCE_PATTERN = /^(`{3,})([\w#+.-]*)[ \t]*(?:\r?\n|$)/
// Closing fence: a whole line of nothing but backticks (per CommonMark it
// must be at least as long as the opening run).
const CLOSING_FENCE_PATTERN = /^`+[ \t]*$/

/** Longest run of consecutive backticks inside the code, 0 when there is none. */
function longestBacktickRun(code: string): number {
  let longest = 0
  for (const match of code.matchAll(/`+/g)) longest = Math.max(longest, match[0].length)
  return longest
}

/**
 * Extracts the fenced block when the whole body is exactly one code fence.
 * Any fence length >= 3 backticks is accepted, so historical triple-backtick
 * bodies and the serializer's grown ```` / ````` fences both parse.
 */
export function extractSingleFencedCode(body: string): FencedCode | null {
  const text = body.trim()
  const opening = OPENING_FENCE_PATTERN.exec(text)
  if (!opening) return null
  const fenceLength = opening[1]?.length ?? 0
  const language = opening[2] ?? ''
  const lines = text.slice(opening[0].length).split('\n')
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index] ?? ''
    const closing = CLOSING_FENCE_PATTERN.exec(line.replace(/\r$/, ''))
    if (!closing || closing[0].replace(/[ \t]/g, '').length < fenceLength) continue
    // The fenced block must span the whole body: only whitespace may follow.
    if (lines.slice(index + 1).some((remaining) => remaining.trim() !== '')) return null
    // The serializer joins the code with "\n" + closing fence, so the lines
    // before the fence ARE the code; a trailing CR from CRLF input is not.
    return { language, code: lines.slice(0, index).join('\n').replace(/\r$/, '') }
  }
  return null
}

/** Registry language of a body that is exactly one fenced code block. */
export function extractFenceLanguage(body: string) {
  const fenced = extractSingleFencedCode(body)
  return fenced ? findCodeLanguage(fenced.language) : null
}

function fenceCode(fenceLanguage: string, code: string): string {
  // The outer fence must out-length the longest backtick run in the code,
  // otherwise an embedded ``` would close the block early and corrupt the
  // Markdown. Minimum stays three for compatibility with existing data.
  const fence = '`'.repeat(Math.max(3, longestBacktickRun(code) + 1))
  return `${fence}${fenceLanguage}\n${code}\n${fence}`
}

/**
 * Turns edited text plus a content type into the API payload. Code types wrap
 * into a Markdown fence; if the text is already a single fenced block the
 * fence is rewritten with the selected language, which keeps
 * parse → edit → serialize stable (never double-fenced) and lets a user
 * re-assign an existing block to another language.
 */
export function serializeContent(text: string, typeId: ContentTypeId): SerializedContent {
  const type = findContentType(typeId)
  if (type?.kind === 'code' && text.trim()) {
    const existing = extractSingleFencedCode(text)
    const code = existing ? existing.code : text
    return { body: fenceCode(fenceLanguageOf(type), code), bodyFormat: BodyFormat.MARKDOWN }
  }
  return { body: text, bodyFormat: type?.kind === 'markdown' ? BodyFormat.MARKDOWN : BodyFormat.TEXT }
}

function fenceLanguageOf(type: ContentTypeOption): string {
  const language = findCodeLanguage(type.id)
  return language ? language.fenceLanguage : String(type.id)
}

/**
 * Derives the editing state from stored content. Plain TEXT always edits as
 * 纯文本 — server hints such as detectedLanguage are display-only fallbacks
 * and never rewrite history. A Markdown body that is exactly one fenced block
 * recognized by the registry edits as that code language with the fence
 * stripped; everything else edits as Markdown verbatim.
 */
export function parseStoredContent(body: string | null | undefined, bodyFormat: BodyFormat): ParsedContent {
  const text = body ?? ''
  if (bodyFormat === BodyFormat.MARKDOWN) {
    const fenced = extractSingleFencedCode(text)
    const language = fenced ? findCodeLanguage(fenced.language) : null
    if (fenced && language) return { typeId: language.id, text: fenced.code }
    return { typeId: 'markdown', text }
  }
  return { typeId: 'text', text }
}

/**
 * Copy helper: returns the bare code when the whole body is a single fenced
 * block (any fence token), otherwise null so callers keep normal copy
 * semantics for prose Markdown documents.
 */
export function unwrapSingleFencedCode(body: string | null | undefined): string | null {
  if (!body) return null
  const fenced = extractSingleFencedCode(body)
  return fenced ? fenced.code : null
}
