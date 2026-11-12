export type Shortcut = { keys: string; label: string }
export type CliHint = { cmd: string; description: string }
export type PageHints = { title: string; keys: Shortcut[]; cmds: CliHint[]; tips: string[] }

export const globalShortcuts: Shortcut[] = [
  { keys: '⌘K', label: 'Command palette' },
  { keys: '?', label: 'Shortcuts & commands' },
  { keys: 'G then S', label: 'Go to projects' },
  { keys: 'G then R', label: 'Go to requests' },
  { keys: 'G then H', label: 'Go to health' },
  { keys: 'G then A', label: 'Go to access (members)' },
  { keys: 'J / K', label: 'Next / previous row' },
]

export const pageHints: Record<string, PageHints> = {
  '/': {
    title: 'Projects',
    keys: [
      { keys: 'N', label: 'New project' },
      { keys: '⌘K', label: 'Jump to a project' },
      { keys: 'G then S', label: 'Projects home' },
    ],
    cmds: [
      { cmd: 'clavis init', description: 'Link this directory to a project' },
      { cmd: 'clavis init --project-name "My App"', description: 'Create a project while linking' },
    ],
    tips: [
      'Production environment chips are amber everywhere: the UI, CLI, and emails.',
      'Every project is created with development, staging, and production environments.',
      'Press ? to see every shortcut and the CLI equivalent of this page.',
    ],
  },
  '/projects/$project': {
    title: 'Secrets',
    keys: [
      { keys: 'N', label: 'Add secret' },
      { keys: 'P', label: 'Paste .env' },
      { keys: '/', label: 'Filter keys' },
    ],
    cmds: [
      { cmd: 'clavis secrets ls', description: 'List key names' },
      { cmd: 'clavis secrets get KEY', description: 'Print one value (audited)' },
      { cmd: 'clavis run -- npm run dev', description: 'Inject secrets and mask output' },
    ],
    tips: [
      'Every reveal is audited with the key name and your reason — never the value.',
      'Empty cells show “+ Add”; click one to set the key in that environment.',
      'Click a key name for its history, metadata, and per-environment values.',
      'Paste a whole .env to review a diff before applying it.',
    ],
  },
  '/projects/$project/environments': {
    title: 'Environments',
    keys: [
      { keys: '↑ / ↓', label: 'Reorder environment' },
      { keys: 'N', label: 'New environment' },
    ],
    cmds: [
      { cmd: 'clavis env ls', description: 'List environments' },
      { cmd: 'clavis env diff dev prod', description: 'Compare key names' },
    ],
    tips: [
      'Production is protected by default and cannot be unprotected from this page.',
      'Environment types set default policies; the UI shows when an env deviates.',
      'Deleting an environment removes its secrets; the API refuses to delete the last one.',
    ],
  },
  '/audit': {
    title: 'Audit log',
    keys: [
      { keys: '/', label: 'Filter events' },
      { keys: 'J / K', label: 'Move through rows' },
    ],
    cmds: [{ cmd: 'clavis admin audit verify', description: 'Verify the audit chain' }],
    tips: [
      'The chain badge recomputes every event hash; a break is shown in red.',
      'Secret values never enter the audit schema — only blind-index references.',
      'Authentication events appear in every org the person belongs to.',
    ],
  },
  '/members': {
    title: 'Members',
    keys: [
      { keys: 'N', label: 'Invite member' },
      { keys: '/', label: 'Filter members' },
    ],
    cmds: [
      { cmd: 'clavis members ls', description: 'List members and roles' },
      { cmd: 'clavis access explain', description: 'Explain why someone has access' },
    ],
    tips: [
      'Invite links are single-use, expire, and can be restricted to one email address.',
      'The last Owner cannot be demoted or removed.',
      'Removing a member revokes their sessions and scoped grants immediately.',
    ],
  },
  '/identities': {
    title: 'Agents & machines',
    keys: [{ keys: 'N', label: 'Add agent or machine' }],
    cmds: [
      { cmd: 'clavis identities ls', description: 'List machine and agent identities' },
      { cmd: 'clavis identities revoke', description: 'Revoke an identity' },
    ],
    tips: [
      'Tokens are shown once and stored hashed; rotate instead of sharing.',
      'Scope CI identities to development or staging unless production access is required.',
      'Agent identities will use use-only permissions and command allowlists in M2.',
    ],
  },
  '/settings': {
    title: 'Settings',
    keys: [{ keys: '⌘K', label: 'Command palette' }],
    cmds: [
      { cmd: 'clavis whoami', description: 'Show the signed-in account' },
      { cmd: 'clavis admin rotate-root-key', description: 'Rotate the offline root key' },
    ],
    tips: [
      'Appearance is saved per user, with an org default for new members.',
      'Sessions are server-side and revocable instantly.',
      'SMTP enables password reset and invite delivery.',
    ],
  },
  '/health': {
    title: 'Health',
    keys: [],
    cmds: [{ cmd: 'clavis health', description: 'Print the same findings in a terminal or CI' }],
    tips: [
      'Health is computed from real usage, not a checklist.',
      'Duplicate-value checks are opt-in per project.',
    ],
  },
  '/requests': {
    title: 'Requests',
    keys: [],
    cmds: [{ cmd: 'clavis access explain', description: 'Explain effective access' }],
    tips: [
      'Approvals show the exact diff and require a reason.',
      'Temporary grants expire automatically.',
    ],
  },
  '/shared-sets': {
    title: 'Shared sets',
    keys: [],
    cmds: [{ cmd: 'clavis secrets ls', description: 'List keys in the linked project' }],
    tips: [
      'Shared sets show their blast radius before saving.',
      'Import order determines precedence.',
    ],
  },
}

const fallbackHints: PageHints = {
  title: 'Clavis',
  keys: [],
  cmds: [{ cmd: 'clavis whoami', description: 'Show the signed-in account' }],
  tips: ['Press ? for shortcuts and CLI equivalents.'],
}

export function hintsFor(pathname: string): PageHints {
  const exact = pageHints[pathname]
  if (exact) return exact
  if (pathname.includes('/environments')) {
    return pageHints['/projects/$project/environments'] ?? fallbackHints
  }
  if (pathname.startsWith('/projects/')) {
    return pageHints['/projects/$project'] ?? fallbackHints
  }
  return pageHints['/'] ?? fallbackHints
}

export const defaultTipRotationMs = 12_000
export const hintsEnabledKey = 'clavis.hints'
