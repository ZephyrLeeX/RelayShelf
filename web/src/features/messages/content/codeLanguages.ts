import languages from '../../../../../internal/messages/contenttype/languages.json'

/**
 * Shared registry of code languages offered by the content type pickers.
 *
 * Languages never expand the API contract: a code language is stored as a
 * Markdown fenced block whose fence token is `fenceLanguage`, so the persisted
 * `bodyFormat` stays TEXT / MARKDOWN. Composer, detail editor, message badges,
 * copy helpers and the Markdown renderer all resolve languages through this
 * registry instead of re-deriving their own heuristics.
 */
export interface CodeLanguage {
  id: string
  label: string
  shortLabel: string
  fenceLanguage: string
  aliases: string[]
}

export const CODE_LANGUAGES: CodeLanguage[] = languages

/** Resolves a fence token, detected language, or alias to a registry entry. */
export function findCodeLanguage(token: string | null | undefined): CodeLanguage | null {
  const normalized = token?.trim().toLowerCase()
  if (!normalized) return null
  return CODE_LANGUAGES.find((language) =>
    language.id === normalized || language.fenceLanguage === normalized || language.aliases.includes(normalized)) ?? null
}
