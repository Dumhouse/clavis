import type { ReactNode } from 'react'

/*
 * A small markdown renderer for the CLI reference text (reference.yaml).
 * It covers what that file uses: paragraphs, headings, bullet and numbered
 * lists, fenced code, tables, `code`, **bold** and [links](./target). Raw HTML
 * is never rendered.
 */

type Block =
  | { kind: 'p'; text: string }
  | { kind: 'h'; text: string }
  | { kind: 'ul' | 'ol'; items: string[] }
  | { kind: 'code'; lang: string; code: string }
  | { kind: 'table'; head: string[]; rows: string[][] }

function cells(line: string): string[] {
  return line
    .trim()
    .replace(/^\||\|$/g, '')
    .split(/(?<!\\)\|/)
    .map((cell) => cell.trim().replace(/\\\|/g, '|'))
}

function parse(source: string): Block[] {
  const lines = source.replace(/\r/g, '').split('\n')
  const blocks: Block[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i] ?? ''
    if (line.trim() === '') {
      i++
      continue
    }
    const fence = /^```(\w*)/.exec(line)
    if (fence) {
      const code: string[] = []
      i++
      while (i < lines.length && !(lines[i] ?? '').startsWith('```')) code.push(lines[i++] ?? '')
      i++
      blocks.push({ kind: 'code', lang: fence[1] ?? '', code: code.join('\n') })
      continue
    }
    if (/^#{2,4}\s/.test(line)) {
      blocks.push({ kind: 'h', text: line.replace(/^#+\s*/, '') })
      i++
      continue
    }
    if (line.startsWith('|')) {
      const rows: string[][] = []
      while (i < lines.length && (lines[i] ?? '').startsWith('|'))
        rows.push(cells(lines[i++] ?? ''))
      const [head = [], , ...body] = rows
      blocks.push({ kind: 'table', head, rows: body })
      continue
    }
    const bullet = /^(-|\d+\.)\s/.exec(line)
    if (bullet) {
      const ordered = bullet[1] !== '-'
      const items: string[] = []
      while (i < lines.length) {
        const current = lines[i] ?? ''
        if (/^(-|\d+\.)\s/.test(current)) {
          items.push(current.replace(/^(-|\d+\.)\s+/, ''))
        } else if (/^\s+\S/.test(current) && items.length > 0) {
          items.push(`${items.pop() ?? ''} ${current.trim()}`)
        } else {
          break
        }
        i++
      }
      blocks.push({ kind: ordered ? 'ol' : 'ul', items })
      continue
    }
    const text: string[] = []
    while (i < lines.length) {
      const current = lines[i] ?? ''
      if (current.trim() === '' || /^(```|#{2,4}\s|\||-\s|\d+\.\s)/.test(current)) break
      text.push(current.trim())
      i++
    }
    blocks.push({ kind: 'p', text: text.join(' ') })
  }
  return blocks
}

export type LinkRenderer = (href: string, children: ReactNode, key: string) => ReactNode

export function Inline({ text, renderLink }: { text: string; renderLink: LinkRenderer }) {
  const out: ReactNode[] = []
  const pattern = /`([^`]+)`|\*\*((?:`[^`]*`|[^*])+)\*\*|\[((?:`[^`]*`|[^\]])+)\]\(([^)\s]+)\)/g
  let last = 0
  let match: RegExpExecArray | null
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > last) out.push(text.slice(last, match.index))
    const key = String(match.index)
    if (match[1] !== undefined) {
      out.push(
        <code key={key} className="rounded bg-elev px-1 py-px font-mono text-[0.9em] text-text">
          {match[1]}
        </code>,
      )
    } else if (match[2] !== undefined) {
      out.push(
        <strong key={key} className="font-semibold text-text">
          <Inline text={match[2]} renderLink={renderLink} />
        </strong>,
      )
    } else if (match[3] !== undefined && match[4] !== undefined) {
      out.push(renderLink(match[4], <Inline text={match[3]} renderLink={renderLink} />, key))
    }
    last = pattern.lastIndex
  }
  if (last < text.length) out.push(text.slice(last))
  return <>{out}</>
}

export function DocMarkdown({
  source,
  renderLink,
  renderCode,
}: {
  source: string
  renderLink: LinkRenderer
  renderCode: (code: string, lang: string) => ReactNode
}) {
  return (
    <div className="space-y-3 text-sm leading-relaxed text-muted">
      {parse(source).map((block, index) => {
        const key = String(index)
        switch (block.kind) {
          case 'p':
            return (
              <p key={key}>
                <Inline text={block.text} renderLink={renderLink} />
              </p>
            )
          case 'h':
            return (
              <h3 key={key} className="pt-2 text-sm font-semibold text-text">
                <Inline text={block.text} renderLink={renderLink} />
              </h3>
            )
          case 'ul':
          case 'ol': {
            const List = block.kind
            return (
              <List
                key={key}
                className={
                  block.kind === 'ul'
                    ? 'list-disc space-y-1 pl-5 marker:text-dim'
                    : 'list-decimal space-y-1 pl-5 marker:text-dim'
                }
              >
                {block.items.map((item) => (
                  <li key={item}>
                    <Inline text={item} renderLink={renderLink} />
                  </li>
                ))}
              </List>
            )
          }
          case 'code':
            return <div key={key}>{renderCode(block.code, block.lang)}</div>
          case 'table':
            return (
              <div key={key} className="overflow-x-auto rounded-app-sm border border-border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-panel text-2xs uppercase tracking-[0.08em] text-dim">
                    <tr>
                      {block.head.map((cell) => (
                        <th key={cell} className="px-3 py-2 font-semibold">
                          <Inline text={cell} renderLink={renderLink} />
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-soft bg-surface">
                    {block.rows.map((row) => (
                      <tr key={row.join('|')}>
                        {row.map((cell, cellIndex) => (
                          <td
                            key={`${String(cellIndex)}-${cell}`}
                            className={
                              cellIndex === 0
                                ? 'whitespace-nowrap px-3 py-2 align-top text-text'
                                : 'px-3 py-2 align-top'
                            }
                          >
                            <Inline text={cell} renderLink={renderLink} />
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
        }
      })}
    </div>
  )
}
