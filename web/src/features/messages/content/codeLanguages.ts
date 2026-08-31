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

export const CODE_LANGUAGES: CodeLanguage[] = [
  { id: 'shell', label: 'Shell', shortLabel: 'SH', fenceLanguage: 'bash', aliases: ['sh', 'bash', 'zsh', 'console'] },
  { id: 'python', label: 'Python', shortLabel: 'PY', fenceLanguage: 'python', aliases: ['py'] },
  { id: 'java', label: 'Java', shortLabel: 'JAVA', fenceLanguage: 'java', aliases: [] },
  { id: 'javascript', label: 'JavaScript', shortLabel: 'JS', fenceLanguage: 'javascript', aliases: ['js'] },
  { id: 'typescript', label: 'TypeScript', shortLabel: 'TS', fenceLanguage: 'typescript', aliases: ['ts'] },
  { id: 'json', label: 'JSON', shortLabel: 'JSON', fenceLanguage: 'json', aliases: [] },
  { id: 'yaml', label: 'YAML', shortLabel: 'YML', fenceLanguage: 'yaml', aliases: ['yml'] },
  { id: 'sql', label: 'SQL', shortLabel: 'SQL', fenceLanguage: 'sql', aliases: [] },
  { id: 'go', label: 'Go', shortLabel: 'GO', fenceLanguage: 'go', aliases: ['golang'] },
  { id: 'rust', label: 'Rust', shortLabel: 'RS', fenceLanguage: 'rust', aliases: ['rs'] },
  { id: 'c', label: 'C', shortLabel: 'C', fenceLanguage: 'c', aliases: [] },
  { id: 'cpp', label: 'C++', shortLabel: 'CPP', fenceLanguage: 'cpp', aliases: ['cxx', 'c++'] },
  { id: 'csharp', label: 'C#', shortLabel: 'C#', fenceLanguage: 'csharp', aliases: ['cs', 'c#'] },
  { id: 'powershell', label: 'PowerShell', shortLabel: 'PS', fenceLanguage: 'powershell', aliases: ['ps1', 'pwsh'] },
  { id: 'dockerfile', label: 'Dockerfile', shortLabel: 'DOCKER', fenceLanguage: 'dockerfile', aliases: ['docker'] },
  { id: 'html', label: 'HTML', shortLabel: 'HTML', fenceLanguage: 'html', aliases: [] },
  { id: 'css', label: 'CSS', shortLabel: 'CSS', fenceLanguage: 'css', aliases: [] },
]

/** Resolves a fence token, detected language, or alias to a registry entry. */
export function findCodeLanguage(token: string | null | undefined): CodeLanguage | null {
  const normalized = token?.trim().toLowerCase()
  if (!normalized) return null
  return CODE_LANGUAGES.find((language) =>
    language.id === normalized || language.fenceLanguage === normalized || language.aliases.includes(normalized)) ?? null
}
