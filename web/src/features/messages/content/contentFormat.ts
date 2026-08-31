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

// Matches a body that is nothing but a single fenced block (leading/trailing
// whitespace tolerated, closing newline optional so truncated previews with a
// complete fence still parse).
const SINGLE_FENCE_PATTERN = /^\s*```([\w#+.-]*)[ \t]*\r?\n([\s\S]*?)\r?\n?```\s*$/

/** Extracts the fenced block when the whole body is exactly one code fence. */
export function extractSingleFencedCode(body: string): FencedCode | null {
  const match = SINGLE_FENCE_PATTERN.exec(body)
  if (!match) return null
  return { language: match[1] ?? '', code: match[2] ?? '' }
}

/** Registry language of a body that is exactly one fenced code block. */
export function extractFenceLanguage(body: string) {
  const fenced = extractSingleFencedCode(body)
  return fenced ? findCodeLanguage(fenced.language) : null
}

function fenceCode(fenceLanguage: string, code: string): string {
  return `\`\`\`${fenceLanguage}\n${code}\n\`\`\``
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
