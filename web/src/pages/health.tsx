import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  AlertTriangle,
  ArrowRight,
  Bot,
  CalendarClock,
  Copy,
  EyeOff,
  HeartPulse,
  History,
  Loader2,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'
import { useMemo } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Notice } from '@/components/ui/field'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'

type Finding = components['schemas']['HealthFindingBody']

type Fix = { label: string; to: string; project?: string | undefined }

type CategoryInfo = {
  title: string
  why: string
  icon: LucideIcon
  fix: (finding: Finding) => Fix
}

const categories: Record<string, CategoryInfo> = {
  'no-mfa': {
    title: 'Members without two-factor authentication',
    why: 'A stolen password is enough to read every secret these people can see.',
    icon: ShieldAlert,
    fix: () => ({ label: 'Require MFA for everyone', to: '/settings' }),
  },
  expiring: {
    title: 'Secrets expiring within 30 days',
    why: 'Services using them will start failing when they lapse.',
    icon: CalendarClock,
    fix: (finding) => ({
      label: 'Replace value',
      to: '/projects/$project',
      project: finding.project,
    }),
  },
  'no-rotation-policy': {
    title: 'Projects without rotation',
    why: 'Nothing renews these secrets automatically, so they only change when someone remembers.',
    icon: RefreshCw,
    fix: (finding) => ({
      label: 'Add a rotation policy',
      to: '/projects/$project/rotation',
      project: finding.project,
    }),
  },
  'never-rotated': {
    title: 'Secrets that were never rotated',
    why: 'The original value is still live, including any copy that leaked along the way.',
    icon: History,
    fix: (finding) => ({
      label: 'Set up rotation',
      to: '/projects/$project/rotation',
      project: finding.project,
    }),
  },
  stale: {
    title: 'Old secrets',
    why: 'Long-lived credentials are the most likely to have been copied somewhere.',
    icon: History,
    fix: (finding) => ({ label: 'Rotate', to: '/projects/$project', project: finding.project }),
  },
  'duplicate-value': {
    title: 'Reused values',
    why: 'One leak exposes several keys, and rotating one leaves the others behind.',
    icon: Copy,
    fix: (finding) => ({
      label: 'Review keys',
      to: '/projects/$project',
      project: finding.project,
    }),
  },
  unused: {
    title: 'Unused secrets',
    why: 'Not revealed or injected in 90 days. Delete what nothing needs.',
    icon: EyeOff,
    fix: (finding) => ({ label: 'Review', to: '/projects/$project', project: finding.project }),
  },
  'non-expiring-token': {
    title: 'Machine tokens that never expire',
    why: 'A leaked token keeps working until someone notices and revokes it.',
    icon: Bot,
    fix: () => ({ label: 'Rotate with an expiry', to: '/identities' }),
  },
}

const fallback: CategoryInfo = {
  title: 'Other findings',
  why: '',
  icon: AlertTriangle,
  fix: () => ({ label: 'Open', to: '/' }),
}

export function HealthPage() {
  const { org } = useCurrentOrg()
  const report = useQuery({
    queryKey: ['health', org?.slug],
    enabled: Boolean(org),
    refetchInterval: 60_000,
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/health', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load health.'))
      return result.data
    },
  })

  const score = report.data?.score ?? 0
  const findings = useMemo(() => report.data?.findings ?? [], [report.data])

  const groups = useMemo(() => {
    const map = new Map<string, Finding[]>()
    for (const finding of findings) {
      const list = map.get(finding.category) ?? []
      list.push(finding)
      map.set(finding.category, list)
    }
    return [...map.entries()].sort((a, b) => {
      const critical = (list: Finding[]) => (list.some((f) => f.level === 'critical') ? 0 : 1)
      return critical(a[1]) - critical(b[1]) || b[1].length - a[1].length
    })
  }, [findings])

  const criticalCount = findings.filter((finding) => finding.level === 'critical').length

  return (
    <div className="space-y-5">
      <header>
        <h1 className="flex items-center gap-2 font-head text-2xl font-semibold tracking-tight">
          <HeartPulse className="size-5 text-muted" /> Health
        </h1>
        <p className="mt-1 text-base text-muted">
          What to fix first to keep secrets safe, checked every minute.
        </p>
      </header>

      {report.isLoading && (
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-sm text-muted">
          <Loader2 className="size-4 animate-spin" /> Checking…
        </div>
      )}
      {report.isError && (
        <Notice>
          {report.error instanceof Error ? report.error.message : 'Could not load health.'}
        </Notice>
      )}

      {report.data && (
        <>
          <section className="flex flex-wrap items-center gap-5 rounded-app border border-border bg-surface px-5 py-4">
            <ScoreRing score={score} />
            <div className="min-w-0 flex-1">
              <p className="text-md font-semibold">
                {findings.length === 0
                  ? 'Everything looks healthy'
                  : criticalCount > 0
                    ? `${String(criticalCount)} critical ${criticalCount === 1 ? 'issue needs' : 'issues need'} attention`
                    : `${String(findings.length)} ${findings.length === 1 ? 'thing' : 'things'} to tidy up`}
              </p>
              <p className="text-xs text-dim">
                {report.data.checked} secrets checked across the organization
              </p>
            </div>
          </section>

          {groups.length === 0 && (
            <div className="flex flex-col items-center rounded-app border border-border bg-surface px-6 py-12 text-center">
              <ShieldCheck className="size-8 text-accent-text" />
              <p className="mt-2 text-md font-semibold">No findings</p>
              <p className="mt-1 max-w-sm text-sm text-muted">
                MFA is on, nothing is expiring, and every project rotates its secrets.
              </p>
            </div>
          )}

          <div className="space-y-3">
            {groups.map(([category, list]) => {
              const info = categories[category] ?? fallback
              const critical = list.some((finding) => finding.level === 'critical')
              const fixes = new Map<string, Fix>()
              for (const finding of list) {
                const fix = info.fix(finding)
                fixes.set(`${fix.to}:${fix.project ?? ''}`, fix)
              }
              return (
                <section
                  key={category}
                  aria-labelledby={`health-${category}`}
                  className={cn(
                    'rounded-app border bg-surface',
                    critical ? 'border-danger/40' : 'border-border',
                  )}
                >
                  <div className="flex flex-wrap items-start gap-3 px-4 py-3">
                    <span
                      className={cn(
                        'mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full',
                        critical ? 'bg-danger-bg text-danger' : 'bg-amber-bg text-amber-fg',
                      )}
                    >
                      <info.icon className="size-4" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <h2 id={`health-${category}`} className="text-base font-semibold">
                        {info.title}
                        <span className="ml-2 rounded-full bg-elev px-1.5 text-2xs font-medium text-muted">
                          {list.length}
                        </span>
                      </h2>
                      {info.why && <p className="mt-0.5 text-sm text-muted">{info.why}</p>}
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                      {[...fixes.values()].slice(0, 3).map((fix) => (
                        <FixLink key={`${fix.to}:${fix.project ?? ''}`} fix={fix} />
                      ))}
                    </div>
                  </div>
                  <ul className="divide-y divide-border-soft border-t border-border-soft">
                    {list.slice(0, 8).map((finding, index) => (
                      <li
                        key={`${finding.key ?? finding.subject ?? ''}-${String(index)}`}
                        className="flex flex-wrap items-center gap-x-3 gap-y-0.5 px-4 py-2 text-xs"
                      >
                        <span className="font-mono text-text">
                          {finding.key || finding.subject || finding.project || 'organization'}
                        </span>
                        {(finding.project || finding.env) && (finding.key || finding.subject) && (
                          <span className="text-dim">
                            {finding.project}
                            {finding.env ? ` / ${finding.env}` : ''}
                          </span>
                        )}
                        <span className="ml-auto text-muted">{finding.message}</span>
                      </li>
                    ))}
                    {list.length > 8 && (
                      <li className="px-4 py-2 text-2xs text-dim">and {list.length - 8} more</li>
                    )}
                  </ul>
                </section>
              )
            })}
          </div>
        </>
      )}
    </div>
  )
}

function FixLink({ fix }: { fix: Fix }) {
  const className =
    'inline-flex h-8 items-center gap-1.5 rounded-app-sm border border-border bg-surface px-2.5 text-xs font-medium text-text hover:bg-hover'
  const label = (
    <>
      {fix.label}
      {fix.project && <span className="font-mono text-dim">{fix.project}</span>}
      <ArrowRight className="size-3" />
    </>
  )
  if (fix.project) {
    return (
      <Link to={fix.to} params={{ project: fix.project }} className={className}>
        {label}
      </Link>
    )
  }
  return (
    <Link to={fix.to} className={className}>
      {label}
    </Link>
  )
}

function ScoreRing({ score }: { score: number }) {
  const radius = 26
  const circumference = 2 * Math.PI * radius
  const tone = score >= 85 ? 'var(--accent)' : score >= 60 ? 'var(--prod)' : 'var(--red)'
  return (
    <div
      className="relative size-16"
      role="img"
      aria-label={`Health score ${String(score)} of 100`}
    >
      <svg viewBox="0 0 64 64" className="size-16 -rotate-90">
        <circle cx="32" cy="32" r={radius} fill="none" stroke="var(--border)" strokeWidth="6" />
        <circle
          cx="32"
          cy="32"
          r={radius}
          fill="none"
          stroke={tone}
          strokeWidth="6"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - Math.max(0, Math.min(100, score)) / 100)}
        />
      </svg>
      <span className="absolute inset-0 flex items-center justify-center text-lg font-semibold">
        {score}
      </span>
    </div>
  )
}
