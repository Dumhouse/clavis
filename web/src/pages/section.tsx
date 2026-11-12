import { Construction } from 'lucide-react'

import { Button } from '@/components/ui/button'

const copy: Record<string, { title: string; body: string; milestone: string }> = {
  '/shared-sets': {
    title: 'Shared sets',
    body: 'Org-level secrets imported by many environments. Edit once, update every project that imports the set.',
    milestone: 'Arrives in M2',
  },
  '/health': {
    title: 'Health',
    body: 'Stale and unused secrets, failing syncs, duplicated values, members without MFA, and leaked token detection.',
    milestone: 'Arrives in M2',
  },
  '/requests': {
    title: 'Requests',
    body: 'Access and change requests with diffs, reasons, durations, and an approval inbox.',
    milestone: 'Arrives in M2',
  },
  '/audit': {
    title: 'Audit log',
    body: 'Every read, write, reveal, export, and permission change, with a verifiable hash chain.',
    milestone: 'Core chain is in M0; the UI arrives in M2',
  },
  '/members': {
    title: 'Members',
    body: 'People, groups, roles, and the effective-access explainer that answers “why can Alex read prod?”.',
    milestone: 'Arrives in M2',
  },
  '/identities': {
    title: 'Identities',
    body: 'Machine and agent identities with short-lived tokens, OIDC federation, and command allowlists.',
    milestone: 'Arrives in M2',
  },
  '/settings': {
    title: 'Settings',
    body: 'Organization security policy, SSO/SCIM, appearance, notifications, and audit export.',
    milestone: 'Arrives in M2',
  },
}

export function SectionPage({ path }: { path: string }) {
  const page = copy[path] ?? {
    title: 'Coming soon',
    body: 'This part of Clavis is still being built.',
    milestone: 'Planned',
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="font-head text-2xl font-semibold tracking-tight">{page.title}</h1>
        <p className="mt-1 max-w-2xl text-base text-muted">{page.body}</p>
      </header>

      <section className="flex flex-col items-start gap-3 rounded-app border border-dashed border-border bg-panel p-8">
        <div className="flex size-10 items-center justify-center rounded-app-sm bg-elev">
          <Construction className="size-5 text-dim" />
        </div>
        <div>
          <h2 className="text-base font-semibold">Not built yet</h2>
          <p className="mt-0.5 text-sm text-muted">
            {page.milestone}. The foundation and API are being implemented first.
          </p>
        </div>
        <Button variant="secondary" size="sm" disabled>
          Coming soon
        </Button>
      </section>
    </div>
  )
}
