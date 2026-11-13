import data from './cli-reference.json'

/*
 * The CLI reference, generated from the command tree and
 * internal/cli/reference.yaml by `make docs-cli`. The public docs site renders
 * the same JSON, and `clavis <command> --help` shows the same examples.
 */

export type RefFlag = {
  name: string
  shorthand?: string
  type?: string
  default?: string
  usage: string
}

export type RefExample = { title?: string; run: string }

export type RefCommand = {
  path: string
  slug: string
  parent?: string
  group: string
  usage: string
  summary: string
  description: string
  examples: RefExample[]
  notes?: string
  flags: RefFlag[]
  subcommands: string[]
  related: string[]
  aliases?: string[]
}

export type RefGroup = { id: string; title: string; description: string; commands: string[] }

export type RefTopic = {
  id: string
  title: string
  summary: string
  body: string
  examples?: RefExample[]
}

export type Reference = {
  groups: RefGroup[]
  commands: RefCommand[]
  globalFlags: RefFlag[]
  topics: RefTopic[]
}

export const reference = data as Reference

const byPath = new Map(reference.commands.map((command) => [command.path, command]))
const bySlug = new Map(reference.commands.map((command) => [command.slug, command]))
const topicIds = new Set(reference.topics.map((topic) => topic.id))

export function commandByPath(path: string): RefCommand | undefined {
  return byPath.get(path)
}

/** Top-level commands of a group followed by all their subcommands, depth first. */
export function groupCommands(group: RefGroup): RefCommand[] {
  const out: RefCommand[] = []
  const visit = (path: string) => {
    const command = byPath.get(path)
    if (!command) return
    out.push(command)
    command.subcommands.forEach(visit)
  }
  group.commands.forEach(visit)
  return out
}

/** Public docs site page for a command or topic. */
export const publicDocsBase = 'https://dumhouse.github.io/clavis/reference/'

export type InternalTarget =
  { kind: 'command'; command: RefCommand } | { kind: 'topic'; id: string }

/** Resolves reference.yaml links such as `./env-diff` or `./clavis-yaml`. */
export function resolveReferenceLink(href: string): InternalTarget | undefined {
  const match = /^\.\/([a-z0-9-]+)$/.exec(href)
  if (!match?.[1]) return undefined
  const command = bySlug.get(match[1])
  if (command) return { kind: 'command', command }
  if (topicIds.has(match[1])) return { kind: 'topic', id: match[1] }
  return undefined
}

export function flagSignature(flag: RefFlag): string {
  const name = `--${flag.name}`
  const withShort = flag.shorthand ? `-${flag.shorthand}, ${name}` : name
  return flag.type ? `${withShort} ${flag.type}` : withShort
}
