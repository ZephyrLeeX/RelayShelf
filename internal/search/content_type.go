package search

// JavaScript String.trim whitespace, shared by the editor's fence parser.
const fenceWhitespace = `E' \t\r\n' || chr(11) || chr(12) || U&'\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000\FEFF'`

// Match the content editor and badges: MARKDOWN is code only when its entire
// plaintext body is one recognized backtick fence. The first closing line must
// be at least as long as the opening fence, with only whitespace after it.
// Legacy detection hints apply only to TEXT. Evaluate before cursor/LIMIT so
// classification never loses results or corrupts pagination.
const codeCondition = `(CASE WHEN m.body_format = 'MARKDOWN' THEN
  m.sensitive = false AND EXISTS (
    SELECT 1
    FROM regexp_match(btrim(m.body_plaintext, ` + fenceWhitespace + `),
      E'^(` + "`" + `{3,})([A-Za-z0-9_#+.-]*)[ \\t]*\\r?\\n(.*)$') AS fence(parts)
    CROSS JOIN LATERAL (
      SELECT min(ordinal) FILTER (
        WHERE line ~ E'^` + "`" + `+[ \\t]*\\r?$'
          AND length(btrim(line, E' \t\r')) >= length(parts[1])
      ) AS closing,
      max(ordinal) FILTER (WHERE btrim(line, ` + fenceWhitespace + `) <> '') AS last_content
      FROM unnest(string_to_array(parts[3], E'\n')) WITH ORDINALITY AS lines(line, ordinal)
    ) bounds
    WHERE lower(parts[2]) = ANY(%s::text[]) AND closing = last_content
  )
  ELSE coalesce(lower(m.detected_type), '') = 'code'
    OR coalesce(m.detected_language, '') <> ''
END)`
