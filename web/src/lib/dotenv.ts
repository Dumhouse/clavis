export type DotenvEntry = { key: string; value: string }

export type DotenvResult = {
  entries: DotenvEntry[]
  error: string | null
}

/** Parses .env text for the paste preview. It never executes anything. */
export function parseDotenv(text: string): DotenvResult {
  const entries: DotenvEntry[] = []
  const lines = text.replace(/^\uFEFF/, '').split(/\r?\n/)
  for (let index = 0; index < lines.length; index++) {
    let line = lines[index]?.trim() ?? ''
    if (line === '' || line.startsWith('#')) continue
    if (line.startsWith('export ')) line = line.slice('export '.length).trim()
    const eq = line.indexOf('=')
    if (eq <= 0) {
      return { entries, error: `Line ${String(index + 1)}: expected KEY=VALUE` }
    }
    const key = line.slice(0, eq).trim()
    if (key === '') {
      return { entries, error: `Line ${String(index + 1)}: empty key` }
    }
    const raw = line.slice(eq + 1).trim()
    const parsed = parseValue(raw)
    if (parsed.error) {
      return { entries, error: `Line ${String(index + 1)}: ${parsed.error}` }
    }
    entries.push({ key, value: parsed.value })
  }
  return { entries, error: null }
}

function parseValue(raw: string): { value: string; error: string | null } {
  if (raw === '') return { value: '', error: null }
  if (raw.startsWith('"')) {
    if (!raw.endsWith('"') || raw.length < 2)
      return { value: '', error: 'unterminated double quote' }
    const inner = raw.slice(1, -1)
    let out = ''
    for (let i = 0; i < inner.length; i++) {
      const ch = inner.charAt(i)
      if (ch !== '\\') {
        out += ch
        continue
      }
      i++
      if (i >= inner.length) return { value: '', error: 'trailing backslash' }
      const escaped = inner.charAt(i)
      if (escaped === 'n') out += '\n'
      else if (escaped === 'r') out += '\r'
      else if (escaped === 't') out += '\t'
      else out += escaped
    }
    return { value: out, error: null }
  }
  if (raw.startsWith("'")) {
    if (!raw.endsWith("'") || raw.length < 2)
      return { value: '', error: 'unterminated single quote' }
    return { value: raw.slice(1, -1), error: null }
  }
  let value = raw
  for (let i = 1; i < value.length; i++) {
    if (value[i] === '#' && /\s/.test(value[i - 1] ?? '')) {
      value = value.slice(0, i).trim()
      break
    }
  }
  return { value, error: null }
}
