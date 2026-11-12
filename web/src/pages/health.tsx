import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, HeartPulse, Loader2, ShieldAlert, ShieldCheck } from 'lucide-react'

import { api } from '@/api/client'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'

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
  const findings = report.data?.findings ?? []

  return (
    <div className="space-y-5">
      <header>
        <h1 className="flex items-center gap-2 font-head text-[25px] font-semibold tracking-tight">
          <HeartPulse className="size-5 text-muted" /> Health
        </h1>
        <p className="mt-1 text-[14px] text-muted">
          Stale secrets, missing rotation policies, expiries, MFA gaps, and non-expiring tokens.
        </p>
      </header>

      {report.isLoading && (
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-[13px] text-muted">
          <Loader2 className="size-4 animate-spin" /> Checking…
        </div>
      )}
      {report.isError && (
        <p className="rounded-app bg-danger-bg px-4 py-3 text-[13px] text-danger">
          {report.error instanceof Error ? report.error.message : 'Could not load health.'}
        </p>
      )}

      {report.data && (
        <>
          <section className="flex flex-wrap items-center gap-5 rounded-app border border-border bg-surface px-5 py-4">
            <div className="flex items-center gap-3">
              <div
                className={cn(
                  'flex size-16 items-center justify-center rounded-full border-4 text-[21px] font-semibold',
                  score >= 85
                    ? 'border-accent-line text-accent-text'
                    : score >= 60
                      ? 'border-amber-fg text-amber-fg'
                      : 'border-danger text-danger',
                )}
              >
                {score}
              </div>
              <div>
                <p className="text-[13px] font-medium">Health score</p>
                <p className="text-[12px] text-dim">{report.data.checked} secrets checked</p>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              {(['critical', 'warning'] as const).map((level) => {
                const count = findings.filter((finding) => finding.level === level).length
                return (
                  <span
                    key={level}
                    className={cn(
                      'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[12px]',
                      level === 'critical'
                        ? 'border-danger/40 bg-danger-bg text-danger'
                        : 'border-amber-fg/40 bg-amber-bg text-amber-fg',
                    )}
                  >
                    {level === 'critical' ? (
                      <ShieldAlert className="size-3.5" />
                    ) : (
                      <AlertTriangle className="size-3.5" />
                    )}
                    {count} {level}
                  </span>
                )
              })}
              {findings.length === 0 && (
                <span className="inline-flex items-center gap-1.5 rounded-full border border-accent-line bg-accent-soft px-2.5 py-1 text-[12px] text-accent-text">
                  <ShieldCheck className="size-3.5" /> No findings
                </span>
              )}
            </div>
          </section>

          <section className="overflow-x-auto rounded-app border border-border bg-surface">
            <table className="w-full min-w-[820px] border-collapse text-left">
              <thead>
                <tr className="border-b border-border text-[11.5px] uppercase tracking-[0.06em] text-dim">
                  <th className="px-4 py-2.5 font-semibold">Level</th>
                  <th className="px-4 py-2.5 font-semibold">Category</th>
                  <th className="px-4 py-2.5 font-semibold">Resource</th>
                  <th className="px-4 py-2.5 font-semibold">Location</th>
                  <th className="px-4 py-2.5 font-semibold">Detail</th>
                </tr>
              </thead>
              <tbody>
                {findings.map((finding, index) => (
                  <tr
                    key={`${finding.category}-${finding.key ?? finding.subject ?? String(index)}`}
                    className="border-b border-border-soft last:border-0"
                  >
                    <td className="px-4 py-2">
                      <span
                        className={cn(
                          'rounded px-1.5 py-0.5 text-[11px] font-medium',
                          finding.level === 'critical'
                            ? 'bg-danger-bg text-danger'
                            : 'bg-amber-bg text-amber-fg',
                        )}
                      >
                        {finding.level}
                      </span>
                    </td>
                    <td className="px-4 py-2 font-mono text-[11.5px] text-muted">
                      {finding.category}
                    </td>
                    <td className="px-4 py-2 font-mono text-[12px]">
                      {finding.key || finding.subject || '—'}
                    </td>
                    <td className="px-4 py-2 text-[12px] text-muted">
                      {finding.project
                        ? `${finding.project}${finding.env ? `/${finding.env}` : ''}`
                        : 'organization'}
                    </td>
                    <td className="px-4 py-2 text-[12px] text-muted">{finding.message}</td>
                  </tr>
                ))}
                {findings.length === 0 && (
                  <tr>
                    <td colSpan={5} className="px-4 py-10 text-center text-[13px] text-muted">
                      Everything looks healthy.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </section>
        </>
      )}
    </div>
  )
}
