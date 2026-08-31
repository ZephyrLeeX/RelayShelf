import { describe, expect, it } from 'vitest'
import { BodyFormat } from '@/api/generated'
import { findCodeLanguage } from './codeLanguages'
import {
  CONTENT_TYPES,
  extractFenceLanguage,
  findContentType,
  parseStoredContent,
  serializeContent,
  unwrapSingleFencedCode,
} from './contentFormat'

describe('codeLanguages registry', () => {
  it('resolves ids, fence languages, and aliases to one entry', () => {
    expect(findCodeLanguage('shell')?.fenceLanguage).toBe('bash')
    expect(findCodeLanguage('bash')?.id).toBe('shell')
    expect(findCodeLanguage('PY')?.id).toBe('python')
    expect(findCodeLanguage('c++')?.id).toBe('cpp')
    expect(findCodeLanguage('c#')?.id).toBe('csharp')
    expect(findCodeLanguage('')).toBeNull()
    expect(findCodeLanguage('nope')).toBeNull()
  })

  it('offers the required content types without duplicates', () => {
    const labels = CONTENT_TYPES.map((type) => type.label)
    for (const label of ['纯文本', 'Markdown', 'Shell', 'Python', 'Java', 'JavaScript', 'TypeScript', 'JSON', 'YAML', 'SQL', 'Go', 'Rust', 'C#', 'PowerShell', 'Dockerfile', 'HTML', 'CSS']) {
      expect(labels).toContain(label)
    }
    expect(new Set(CONTENT_TYPES.map((type) => type.id)).size).toBe(CONTENT_TYPES.length)
    expect(findContentType('java')?.kind).toBe('code')
    expect(findContentType('markdown')?.kind).toBe('markdown')
    expect(findContentType('text')?.kind).toBe('plain')
  })
})

describe('serializeContent', () => {
  it('keeps plain text and markdown on their contract formats', () => {
    expect(serializeContent('hello', 'text')).toEqual({ body: 'hello', bodyFormat: BodyFormat.TEXT })
    expect(serializeContent('# hello', 'markdown')).toEqual({ body: '# hello', bodyFormat: BodyFormat.MARKDOWN })
  })

  it('wraps Shell, Python, and Java into fenced Markdown', () => {
    expect(serializeContent('docker compose ps', 'shell')).toEqual({
      body: '```bash\ndocker compose ps\n```',
      bodyFormat: BodyFormat.MARKDOWN,
    })
    expect(serializeContent('print("hello")', 'python')).toEqual({
      body: '```python\nprint("hello")\n```',
      bodyFormat: BodyFormat.MARKDOWN,
    })
    expect(serializeContent('class A {}', 'java')).toEqual({
      body: '```java\nclass A {}\n```',
      bodyFormat: BodyFormat.MARKDOWN,
    })
  })

  it('never double fences an already fenced body and can reassign the language', () => {
    const stored = serializeContent('x = 1', 'python')
    const roundTrip = serializeContent(stored.body, 'python')
    expect(roundTrip).toEqual(stored)

    const reassigned = serializeContent(stored.body, 'shell')
    expect(reassigned).toEqual({ body: '```bash\nx = 1\n```', bodyFormat: BodyFormat.MARKDOWN })
  })

  it('preserves code content verbatim through the fence', () => {
    const code = 'line one\n\nline\ttwo '
    const stored = serializeContent(code, 'sql')
    const parsed = parseStoredContent(stored.body, stored.bodyFormat)
    expect(parsed.text).toBe(code)
    expect(serializeContent(parsed.text, parsed.typeId)).toEqual(stored)
  })
})

describe('parseStoredContent', () => {
  it('edits plain TEXT as 纯文本 regardless of detection hints', () => {
    expect(parseStoredContent('sudo systemctl restart nginx', BodyFormat.TEXT)).toEqual({
      typeId: 'text',
      text: 'sudo systemctl restart nginx',
    })
  })

  it('edits ordinary Markdown verbatim', () => {
    const body = '# title\n\nsome prose'
    expect(parseStoredContent(body, BodyFormat.MARKDOWN)).toEqual({ typeId: 'markdown', text: body })
  })

  it('recognizes a single fenced block as its code language with the fence stripped', () => {
    expect(parseStoredContent('```python\nprint("x")\n```', BodyFormat.MARKDOWN)).toEqual({
      typeId: 'python',
      text: 'print("x")',
    })
    expect(parseStoredContent('```bash\nls -la\n```', BodyFormat.MARKDOWN)).toEqual({
      typeId: 'shell',
      text: 'ls -la',
    })
  })

  it('keeps prose Markdown with embedded fences as Markdown', () => {
    const body = 'intro\n\n```python\nx\n```\n\noutro'
    expect(parseStoredContent(body, BodyFormat.MARKDOWN)).toEqual({ typeId: 'markdown', text: body })
  })

  it('falls back to Markdown for an unrecognized fence language', () => {
    const body = '```brainfuck\n+++\n```'
    expect(parseStoredContent(body, BodyFormat.MARKDOWN)).toEqual({ typeId: 'markdown', text: body })
  })

  it('keeps parse → edit → serialize stable across re-edits', () => {
    // Historical TEXT reassigned to Shell, then edited twice more.
    let stored = serializeContent('sudo nginx -s reload', 'shell')
    for (const edit of ['sudo nginx -t', 'systemctl restart nginx']) {
      const parsed = parseStoredContent(stored.body, stored.bodyFormat)
      expect(parsed.typeId).toBe('shell')
      stored = serializeContent(edit, parsed.typeId)
    }
    expect(stored).toEqual({ body: '```bash\nsystemctl restart nginx\n```', bodyFormat: BodyFormat.MARKDOWN })
  })

  it('reassigns a historical TEXT body to Shell/Python through the edit pipeline', () => {
    const legacy = parseStoredContent('sudo systemctl restart nginx', BodyFormat.TEXT)
    expect(legacy.typeId).toBe('text')

    const stored = serializeContent('sudo systemctl restart nginx', 'shell')
    expect(stored).toEqual({ body: '```bash\nsudo systemctl restart nginx\n```', bodyFormat: BodyFormat.MARKDOWN })
    // Reopening the saved message recognizes the code language and the bare code.
    expect(parseStoredContent(stored.body, stored.bodyFormat)).toEqual({
      typeId: 'shell',
      text: 'sudo systemctl restart nginx',
    })
    const python = serializeContent('print("x")', 'python')
    expect(parseStoredContent(python.body, python.bodyFormat)).toEqual({ typeId: 'python', text: 'print("x")' })
  })
})

describe('fence helpers', () => {
  it('extracts the fence language for badges', () => {
    expect(extractFenceLanguage('```java\nx\n```')?.shortLabel).toBe('JAVA')
    expect(extractFenceLanguage('plain text')).toBeNull()
    expect(extractFenceLanguage('a\n```java\nx\n```')).toBeNull()
  })

  it('unwraps exactly one fenced block for copy, any language', () => {
    expect(unwrapSingleFencedCode('```bash\ndocker compose up -d\n```')).toBe('docker compose up -d')
    expect(unwrapSingleFencedCode('```unknown\ncode\n```')).toBe('code')
    expect(unwrapSingleFencedCode('prose\n```bash\ncmd\n```\nmore')).toBeNull()
    expect(unwrapSingleFencedCode(null)).toBeNull()
  })
})
