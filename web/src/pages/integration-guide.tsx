import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CircleAlert,
  CloudDownload,
  ExternalLink,
  Loader2,
  Send,
  Sparkles,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { CopyBlock, CopyCommand } from '@/components/copy-command'
import { Button } from '@/components/ui/button'
import { Field, Input, Notice, Select } from '@/components/ui/field'
import { Modal, ModalHeader } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import {
  ciSnippet,
  defaultBranch,
  methodInfo,
  mirrorPath,
  platforms,
  secretNameFor,
  type ActionSource,
  type Method,
  type Platform,
} from '@/lib/integration-guide'
import { useSelfSignedFingerprint } from '@/lib/server-certificate'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']

export function PlatformMark({
  platform,
  size = 'md',
}: {
  platform: Platform
  size?: 'sm' | 'md'
}) {
  return (
    <span
      aria-hidden
      className={cn(
        'flex shrink-0 items-center justify-center bg-elev font-mono font-bold',
        size === 'sm' ? 'size-6 rounded-md text-3xs' : 'size-9 rounded-app-sm text-2xs',
      )}
      style={{ color: platform.color }}
    >
      {platform.mark}
    </span>
  )
}

/**
 * "Set up an integration": pick a platform, see whether Clavis should push
 * values there (sync) or the job should fetch them (CI access) and why, then
 * either create the CI tokens and workflow here or continue to the sync form.
 */
export function SetupGuide({
  orgSlug,
  projectSlug,
  envs,
  start,
  onClose,
  onPush,
  onCreated,
}: {
  orgSlug: string
  projectSlug: string
  envs: Env[]
  start?: { platform?: string | undefined; method?: Method }
  onClose: () => void
  /** Continue with the sync form for this provider id. */
  onPush: (provider: string) => void
  onCreated: () => void
}) {
  const initial = platforms.find((candidate) => candidate.id === start?.platform) ?? null
  const [platform, setPlatform] = useState<Platform | null>(initial)
  const [step, setStep] = useState<'platform' | 'method' | 'fetch'>(
    initial ? (start?.method === 'fetch' ? 'fetch' : 'method') : 'platform',
  )

  const choices = platforms.filter((candidate) =>
    start?.method === 'fetch'
      ? Boolean(candidate.ci)
      : start?.method === 'push'
        ? Boolean(candidate.sync)
        : true,
  )

  return (
    <Modal onClose={onClose} className="max-w-5xl">
      <ModalHeader
        title={
          <span className="flex items-center gap-2.5">
            {platform && step !== 'platform' && <PlatformMark platform={platform} size="sm" />}
            {step === 'platform'
              ? start?.method === 'push'
                ? 'Add a sync'
                : start?.method === 'fetch'
                  ? 'Add CI access'
                  : 'Set up an integration'
              : step === 'method'
                ? `${platform?.name ?? ''}: how should it get secrets?`
                : `${platform?.name ?? ''}: fetch secrets when the job runs`}
          </span>
        }
        description={
          step === 'platform'
            ? start?.method === 'push'
              ? 'Where should Clavis copy the secrets? For CI pipelines, Clavis first explains why fetching may fit better.'
              : start?.method === 'fetch'
                ? 'Which CI runs the jobs? Clavis creates a token per environment and writes the workflow.'
                : 'Where should your secrets be used? Clavis suggests the best way for that platform and walks you through it.'
            : undefined
        }
        onClose={onClose}
      />
      <Stepper step={step} />
      <div className="mt-5">
        {step === 'platform' && (
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {choices.map((candidate) => (
              <button
                key={candidate.id}
                type="button"
                onClick={() => {
                  setPlatform(candidate)
                  // A chosen way skips the advice, unless it goes against it.
                  if (start?.method === 'fetch') setStep('fetch')
                  else if (
                    start?.method === 'push' &&
                    candidate.recommend === 'push' &&
                    candidate.sync
                  )
                    onPush(candidate.sync)
                  else setStep('method')
                }}
                className="group flex items-start gap-3 rounded-app border border-border bg-surface p-3 text-left transition-colors hover:border-accent-line hover:bg-hover-soft"
              >
                <PlatformMark platform={candidate} />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium">{candidate.name}</span>
                  <span className="mt-0.5 flex items-center gap-1 text-2xs text-dim">
                    {candidate.recommend === 'fetch' ? (
                      <CloudDownload className="size-3" />
                    ) : (
                      <Send className="size-3" />
                    )}
                    Suggested: {methodInfo[candidate.recommend].title.toLowerCase()}
                  </span>
                </span>
                <ArrowRight className="size-4 text-dim group-hover:text-accent-text" />
              </button>
            ))}
          </div>
        )}

        {step === 'method' && platform && (
          <MethodChoice
            platform={platform}
            onBack={() => {
              setStep('platform')
            }}
            onFetch={() => {
              setStep('fetch')
            }}
            onPush={() => {
              if (platform.sync) onPush(platform.sync)
            }}
          />
        )}

        {step === 'fetch' && platform && (
          <FetchSetup
            orgSlug={orgSlug}
            projectSlug={projectSlug}
            envs={envs}
            platform={platform}
            onBack={() => {
              setStep('method')
            }}
            onCreated={onCreated}
            onDone={onClose}
          />
        )}
      </div>
    </Modal>
  )
}

function Stepper({ step }: { step: 'platform' | 'method' | 'fetch' }) {
  const steps = [
    ['platform', 'Platform'],
    ['method', 'Recommended way'],
    ['fetch', 'Set it up'],
  ] as const
  const at = steps.findIndex(([id]) => id === step)
  return (
    <ol className="mt-4 flex items-center gap-2 text-2xs">
      {steps.map(([id, label], index) => (
        <li key={id} className="flex items-center gap-2">
          <span
            className={cn(
              'flex size-5 items-center justify-center rounded-full font-mono text-3xs font-semibold',
              index < at
                ? 'bg-success-bg text-success'
                : index === at
                  ? 'bg-accent-soft text-accent-text'
                  : 'bg-elev text-dim',
            )}
          >
            {index < at ? <Check className="size-3" /> : index + 1}
          </span>
          <span className={index === at ? 'font-medium text-text' : 'text-dim'}>{label}</span>
          {index < steps.length - 1 && <span className="h-px w-6 bg-border" />}
        </li>
      ))}
    </ol>
  )
}

function MethodChoice({
  platform,
  onBack,
  onFetch,
  onPush,
}: {
  platform: Platform
  onBack: () => void
  onFetch: () => void
  onPush: () => void
}) {
  const card = (method: Method, available: boolean, unavailable: string, action: () => void) => {
    const info = methodInfo[method]
    const recommended = platform.recommend === method
    return (
      <div
        className={cn(
          'flex flex-col rounded-app border p-4',
          recommended ? 'border-accent-line bg-accent-soft/40' : 'border-border bg-surface',
          !available && 'opacity-60',
        )}
      >
        <div className="flex items-center gap-2">
          {method === 'fetch' ? (
            <CloudDownload className="size-4 text-accent-text" />
          ) : (
            <Send className="size-4 text-muted" />
          )}
          <h3 className="text-sm font-semibold">{info.title}</h3>
          {recommended && (
            <span className="inline-flex items-center gap-1 rounded-full bg-accent-soft px-2 py-0.5 text-3xs font-semibold uppercase tracking-[0.06em] text-accent-text">
              <Sparkles className="size-3" /> Recommended
            </span>
          )}
        </div>
        <p className="mt-1 text-xs text-muted">{info.short}</p>
        <ol className="mt-3 list-decimal space-y-1 pl-4 text-xs text-muted marker:text-dim">
          {info.how.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ol>
        <div className="mt-auto pt-4">
          {available ? (
            <Button variant={recommended ? 'primary' : 'secondary'} size="sm" onClick={action}>
              {method === 'fetch' ? 'Set up fetching' : 'Set up a sync'}{' '}
              <ArrowRight className="size-3.5" />
            </Button>
          ) : (
            <p className="text-2xs text-dim">{unavailable}</p>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-3 md:grid-cols-2">
        {card(
          'fetch',
          Boolean(platform.ci),
          `${platform.name} runs your app with its own settings, so it can’t fetch from Clavis first.`,
          onFetch,
        )}
        {card('push', Boolean(platform.sync), 'Clavis can’t push to this platform.', onPush)}
      </div>
      <div className="rounded-app border border-border bg-panel p-4">
        <h3 className="text-sm font-semibold">Why we suggest this for {platform.name}</h3>
        <p className="mt-1 text-xs text-muted">{platform.why}</p>
        {platform.limits.length > 0 && (
          <>
            <h4 className="mt-3 flex items-center gap-1.5 text-2xs font-semibold uppercase tracking-[0.06em] text-dim">
              <CircleAlert className="size-3.5" /> Good to know
            </h4>
            <ul className="mt-1.5 list-disc space-y-1 pl-4 text-xs text-muted marker:text-dim">
              {platform.limits.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </>
        )}
      </div>
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft className="size-3.5" /> Other platform
      </Button>
    </div>
  )
}

type Created = { env: string; secret: string; token: string; identity: string }

const expiryChoices: Array<[string, string]> = [
  ['90', '90 days'],
  ['180', '6 months'],
  ['365', '1 year'],
]

/** Platforms whose repository secrets Clavis can write (same code as syncs). */
const storable = new Set(['forgejo', 'github', 'gitlab'])

type RepoRef = { origin: string; path: string; apiBase: string }

/** owner/name (or a GitLab group path) and the API base from a repository URL. */
function parseRepo(raw: string, provider: string): RepoRef | null {
  let url: URL
  try {
    url = new URL(raw.trim())
  } catch {
    return null
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return null
  const path = url.pathname
    .replace(/^\/+|\/+$/g, '')
    .replace(/\.git$/, '')
    .replace(/\/-\/.*$/, '')
  const parts = path.split('/').filter(Boolean)
  if (parts.length < 2) return null
  const repoPath = provider === 'gitlab' ? parts.join('/') : parts.slice(0, 2).join('/')
  const apiBase =
    provider === 'github' ? (url.host === 'github.com' ? '' : `${url.origin}/api/v3`) : url.origin
  return { origin: url.origin, path: repoPath, apiBase }
}

function sameOrigin(a: string, b: string) {
  try {
    const left = new URL(a)
    const right = new URL(b)
    return (
      left.host === right.host || (right.host === 'api.github.com' && left.host === 'github.com')
    )
  } catch {
    return false
  }
}

function FetchSetup({
  orgSlug,
  projectSlug,
  envs,
  platform,
  onBack,
  onCreated,
  onDone,
}: {
  orgSlug: string
  projectSlug: string
  envs: Env[]
  platform: Platform
  onBack: () => void
  onCreated: () => void
  onDone: () => void
}) {
  const queryClient = useQueryClient()
  const fingerprint = useSelfSignedFingerprint()
  const defaults = envs.filter((env) => env.slug !== 'dev' && env.slug !== 'development')
  const [picked, setPicked] = useState<string[]>(
    (defaults.length > 0 ? defaults : envs).map((env) => env.slug),
  )
  const [days, setDays] = useState('365')
  const [repoUrl, setRepoUrl] = useState('')
  const [branches, setBranches] = useState<Record<string, string>>({})
  const [actionSource, setActionSource] = useState<ActionSource>('github')
  const canStore = Boolean(platform.sync && storable.has(platform.sync))
  const [store, setStore] = useState(canStore)
  const [credential, setCredential] = useState<string | null>(null)
  const [accessToken, setAccessToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<Created[]>([])
  const [stored, setStored] = useState<string[] | null>(null)

  const provider = platform.sync ?? ''
  const connections = useQuery({
    queryKey: ['connections', orgSlug, provider],
    enabled: canStore,
    queryFn: async () => {
      const result = await api.GET('/api/v1/orgs/{org}/connections', {
        params: { path: { org: orgSlug }, query: { provider } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load credentials.'))
      return result.data?.connections ?? []
    },
  })

  // Keep the order of the project's environments (staging before prod).
  const chosen = envs.map((env) => env.slug).filter((slug) => picked.includes(slug))
  const secretNames = Object.fromEntries(
    chosen.map((slug) => [slug, secretNameFor(slug, chosen.length > 1)]),
  )
  const branchFor = (env: string) => branches[env] ?? defaultBranch(env)
  const ci = platform.ci ?? 'shell'
  const repo = parseRepo(repoUrl, provider)
  const saved = connections.data ?? []
  // A platform token for the same server is the natural default.
  const matching = repo ? saved.find((item) => sameOrigin(repo.origin, item.base_url)) : saved[0]
  const credentialId = credential ?? matching?.id ?? (saved[0]?.id || 'new')
  // With a platform token, list its repositories so the URL is one pick away.
  const lister = saved.find((item) => item.id === credentialId) ?? saved[0]
  const repositories = useQuery({
    queryKey: ['repositories', orgSlug, projectSlug, lister?.id],
    enabled: canStore && Boolean(lister),
    retry: false,
    queryFn: async () => {
      const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/syncs/repositories', {
        params: { path: { org: orgSlug, project: projectSlug } },
        body: {
          provider: provider as 'forgejo' | 'github' | 'gitlab',
          connection_id: lister?.id ?? '',
        },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not list repositories.'))
      return result.data.repositories ?? []
    },
  })
  const repoChoices = repositories.data ?? []
  const pickedRepo = repo
    ? repoChoices.find((item) => item.full_name.toLowerCase() === repo.path.toLowerCase())
    : undefined
  const snippet = ciSnippet({
    ci,
    server: window.location.origin,
    fingerprint,
    envs: chosen,
    secretNames,
    branches: Object.fromEntries(chosen.map((env) => [env, branchFor(env)])),
    actionSource,
    forgeOrigin: repo?.origin,
  })
  const secretsPage = repo && platform.secretsPage ? platform.secretsPage(repoUrl.trim()) : ''
  const storeReady =
    !store || (repo !== null && (credentialId !== 'new' || accessToken.trim() !== ''))

  async function storeSecrets(items: Created[]) {
    if (!repo) return
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/syncs/ci-secrets', {
      params: { path: { org: orgSlug, project: projectSlug } },
      body: {
        provider: provider as 'forgejo' | 'github' | 'gitlab',
        repo: repo.path,
        secrets: Object.fromEntries(items.map((item) => [item.secret, item.token])),
        ...(credentialId !== 'new'
          ? { connection_id: credentialId }
          : { base_url: repo.apiBase, token: accessToken.trim() }),
      },
    })
    setBusy(false)
    if (result.error) {
      setError(
        `${apiErrorMessage(result.error, 'Could not store the tokens.')} The tokens were created: copy them by hand below, or fix the access and try again.`,
      )
      return
    }
    setStored(result.data?.stored ?? [])
  }

  async function createTokens() {
    setBusy(true)
    setError(null)
    const out: Created[] = []
    for (const env of chosen) {
      const base = `${platform.id}-${projectSlug}-${env}`
      let result: Awaited<ReturnType<typeof createOne>> | null = null
      for (let attempt = 0; attempt < 4; attempt++) {
        const name = attempt === 0 ? base : `${base}-${String(attempt + 1)}`
        result = await createOne(name, env)
        if (result.data || !/exist|taken|duplicate|unique/i.test(result.message)) break
      }
      if (!result?.data) {
        setError(result?.message ?? 'Could not create the token.')
        break
      }
      out.push({
        env,
        secret: secretNames[env] ?? 'CLAVIS_TOKEN',
        token: result.data.token,
        identity: result.data.identity.name,
      })
    }
    setCreated(out)
    setBusy(false)
    await queryClient.invalidateQueries({ queryKey: ['identities', orgSlug] })
    if (out.length > 0) onCreated()
    if (out.length === chosen.length && store) await storeSecrets(out)
  }

  async function createOne(name: string, env: string) {
    const result = await api.POST('/api/v1/orgs/{org}/identities', {
      params: { path: { org: orgSlug } },
      body: {
        name,
        type: 'machine',
        project: projectSlug,
        environment: env,
        role: 'Developer',
        expires_in_days: Number(days),
      },
    })
    return {
      data: result.data ?? null,
      message: result.error ? apiErrorMessage(result.error, 'Could not create the token.') : '',
    }
  }

  const done = created.length > 0 && created.length === chosen.length
  const locked = created.length > 0
  const secretKind = platform.id === 'gitlab' ? 'masked CI/CD variable' : 'repository secret'
  const repoPlaceholder =
    platform.id === 'forgejo'
      ? 'https://git.example.com/team/web'
      : platform.id === 'gitlab'
        ? 'https://gitlab.com/team/web'
        : 'https://github.com/team/web'

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
      <div className="space-y-5">
        <Section n={1} title="Which environments, from which branches?">
          <div className="grid gap-1.5">
            {envs.map((env) => {
              const on = picked.includes(env.slug)
              return (
                <div
                  key={env.id}
                  className={cn(
                    'flex items-center gap-2.5 rounded-app-sm border px-3 py-1.5 text-sm',
                    on ? 'border-accent-line bg-accent-soft' : 'border-border-soft',
                  )}
                >
                  <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2.5">
                    <input
                      type="checkbox"
                      checked={on}
                      disabled={locked}
                      onChange={() => {
                        setPicked((state) =>
                          on ? state.filter((slug) => slug !== env.slug) : [...state, env.slug],
                        )
                      }}
                    />
                    <span className="font-medium">{env.name}</span>
                    <span className="font-mono text-2xs text-dim">{env.slug}</span>
                    {env.type === 'production' && (
                      <span className="rounded-full bg-amber-bg px-1.5 py-0.5 text-3xs font-semibold text-amber-fg">
                        production
                      </span>
                    )}
                  </label>
                  {on && ci !== 'shell' && (
                    <label className="flex items-center gap-1.5 text-2xs text-dim">
                      branch
                      <Input
                        className="h-7 w-32 font-mono text-xs"
                        value={branchFor(env.slug)}
                        placeholder="by hand only"
                        aria-label={`Branch that deploys ${env.slug}`}
                        onChange={(event) => {
                          setBranches((state) => ({ ...state, [env.slug]: event.target.value }))
                        }}
                      />
                    </label>
                  )}
                </div>
              )
            })}
          </div>
          <p className="text-2xs text-dim">
            Each environment gets its own token, which can read only that environment.
            {ci !== 'shell' &&
              ' A push to a branch loads its environment. Leave a branch empty to deploy that environment only by hand (Run workflow).'}
          </p>
        </Section>

        {ci !== 'shell' && (
          <Section n={2} title="Repository">
            <Field
              label="Repository URL"
              hint={
                canStore
                  ? 'Clavis stores the tokens there for you, and links its secrets page.'
                  : 'Used to link its secrets page.'
              }
            >
              <Input
                value={repoUrl}
                disabled={locked}
                placeholder={repoPlaceholder}
                onChange={(event) => {
                  setRepoUrl(event.target.value)
                }}
              />
            </Field>
            {lister && (repositories.isLoading || repoChoices.length > 0 || repositories.error) && (
              <Field label="Or pick one" hint={`Repositories “${lister.name}” can see.`}>
                {repositories.isLoading ? (
                  <p className="flex items-center gap-1.5 text-xs text-muted">
                    <Loader2 className="size-3.5 animate-spin" /> Loading repositories…
                  </p>
                ) : repositories.error ? (
                  <p className="text-xs text-red-fg">{repositories.error.message}</p>
                ) : (
                  <Select
                    value={pickedRepo?.full_name ?? ''}
                    disabled={locked}
                    onChange={(event) => {
                      const item = repoChoices.find((r) => r.full_name === event.target.value)
                      if (!item) return
                      const base = lister.base_url.replace(/\/+$/, '').replace(/\/api\/v\d+$/, '')
                      setRepoUrl(item.url || `${base}/${item.full_name}`)
                      setCredential(lister.id)
                    }}
                  >
                    <option value="">Choose a repository…</option>
                    {repoChoices.map((item) => (
                      <option key={item.full_name} value={item.full_name}>
                        {item.full_name}
                        {item.private ? ' · private' : ''}
                        {item.can_write ? '' : ' · read only'}
                      </option>
                    ))}
                  </Select>
                )}
              </Field>
            )}
            {store && pickedRepo && !pickedRepo.can_write && (
              <Notice tone="warning">
                “{lister?.name}” can read {pickedRepo.full_name} but probably can’t manage its
                secrets. Storing the tokens needs admin rights on the repository (Forgejo:
                write:repository scope); otherwise add them by hand.
              </Notice>
            )}
            {repoUrl.trim() && !repo && (
              <p className="text-2xs text-red-fg">
                Paste the repository’s web address, like {repoPlaceholder}.
              </p>
            )}
            {platform.ci === 'forgejo' && (
              <fieldset>
                <legend className="text-sm font-medium">How the job loads secrets</legend>
                <p className="mb-2 mt-0.5 text-2xs text-dim">
                  Your workflow runs on Forgejo either way. Forgejo has no action store of its own:
                  a workflow names an action by its Git address, and the Clavis action is published
                  in the Clavis repository on GitHub.
                </p>
                <div className="grid gap-1.5">
                  {(
                    [
                      [
                        'github',
                        'Clavis action, loaded from GitHub',
                        'The runner fetches the action from github.com/dumhouse/clavis when the job starts, pinned to a release tag. Simplest, if the runner can reach GitHub.',
                      ],
                      [
                        'mirror',
                        `Clavis action, from a mirror on ${repo ? new URL(repo.origin).host : 'your Forgejo'}`,
                        `For runners without internet access. Create a pull mirror of github.com/dumhouse/clavis as ${mirrorPath} once (New migration → GitHub → “This repository will be a mirror”); Forgejo keeps it current.`,
                      ],
                      [
                        'plain',
                        'No action: plain shell steps',
                        'Each step runs the clavis CLI directly, nothing to fetch but the CLI itself (skipped if the runner image has it). Easiest to read and audit.',
                      ],
                    ] as const
                  ).map(([value, label, hint]) => (
                    <label
                      key={value}
                      className={cn(
                        'flex cursor-pointer items-start gap-2.5 rounded-app-sm border px-3 py-2 text-sm',
                        actionSource === value
                          ? 'border-accent-line bg-accent-soft'
                          : 'border-border-soft hover:bg-hover',
                      )}
                    >
                      <input
                        type="radio"
                        className="mt-1 shrink-0"
                        checked={actionSource === value}
                        onChange={() => {
                          setActionSource(value)
                        }}
                      />
                      <span>
                        {label}
                        <span className="block text-2xs text-dim">{hint}</span>
                      </span>
                    </label>
                  ))}
                </div>
              </fieldset>
            )}
          </Section>
        )}

        <Section n={ci === 'shell' ? 2 : 3} title="Create the tokens">
          <Field label="Token lifetime">
            <Select
              value={days}
              disabled={locked}
              onChange={(event) => {
                setDays(event.target.value)
              }}
            >
              {expiryChoices.map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </Select>
          </Field>

          {canStore && !locked && (
            <div className="space-y-2 rounded-app-sm border border-border-soft p-3">
              <label className="flex cursor-pointer items-start gap-2.5 text-sm">
                <input
                  type="checkbox"
                  className="mt-1 shrink-0"
                  checked={store}
                  onChange={(event) => {
                    setStore(event.target.checked)
                  }}
                />
                <span>
                  Store them in the repository for me
                  <span className="block text-2xs text-dim">
                    Clavis writes each token as a {secretKind} (
                    {chosen.map((env) => secretNames[env]).join(', ') || 'CLAVIS_TOKEN'}) and keeps
                    no copy. Needs an access token that can manage the repository’s secrets, used
                    once.
                  </span>
                </span>
              </label>
              {store && (
                <>
                  <Field label={`${platform.name.split(' ')[0] ?? ''} access`}>
                    <Select
                      value={credentialId}
                      onChange={(event) => {
                        setCredential(event.target.value)
                      }}
                    >
                      {saved.map((item) => (
                        <option key={item.id} value={item.id}>
                          {item.name} · platform token
                        </option>
                      ))}
                      <option value="new">Enter an access token</option>
                    </Select>
                  </Field>
                  {credentialId === 'new' && (
                    <Input
                      type="password"
                      autoComplete="off"
                      value={accessToken}
                      placeholder={
                        platform.id === 'github'
                          ? 'Fine-grained token with Secrets: read and write'
                          : platform.id === 'gitlab'
                            ? 'Token with the api scope (Maintainer)'
                            : 'Token with the write:repository scope'
                      }
                      onChange={(event) => {
                        setAccessToken(event.target.value)
                      }}
                    />
                  )}
                  {!repo && (
                    <p className="text-2xs text-dim">Enter the repository URL above first.</p>
                  )}
                </>
              )}
            </div>
          )}

          {!locked && (
            <Button
              variant="primary"
              size="sm"
              disabled={busy || chosen.length === 0 || !storeReady}
              onClick={() => {
                void createTokens()
              }}
            >
              {busy && <Loader2 className="size-3.5 animate-spin" />} Create{' '}
              {chosen.length === 1 ? 'token' : `${String(chosen.length)} tokens`}
              {store && canStore ? ' and store them' : ''}
            </Button>
          )}

          {stored && repo && (
            <Notice tone="success">
              Stored {stored.join(', ')} in {repo.path}. Nothing to copy: commit the workflow and
              push.
            </Notice>
          )}

          {locked && !stored && (
            <div className="space-y-3">
              <Notice tone="warning">
                Copy each token now: Clavis shows it only once. Add it as a {secretKind} with the
                name shown.
              </Notice>
              {created.map((item) => (
                <div
                  key={item.env}
                  className="space-y-1.5 rounded-app-sm border border-border-soft p-3"
                >
                  <p className="flex flex-wrap items-center gap-2 text-xs">
                    <span className="font-mono text-2xs text-dim">{item.env}</span>
                    <span className="text-dim">→ secret</span>
                    <span className="font-mono font-semibold text-text">{item.secret}</span>
                  </p>
                  <CopyCommand command={item.token} />
                </div>
              ))}
              {secretsPage && (
                <a
                  href={secretsPage}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 text-xs font-medium text-accent-text hover:underline"
                >
                  Open the repository’s secrets page <ExternalLink className="size-3" />
                </a>
              )}
              {store && canStore && done && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={busy || !storeReady}
                  onClick={() => {
                    void storeSecrets(created)
                  }}
                >
                  {busy && <Loader2 className="size-3.5 animate-spin" />} Try storing them again
                </Button>
              )}
            </div>
          )}
          {error && <Notice>{error}</Notice>}
          {locked && (
            <p className="text-2xs text-dim">
              The tokens are listed under CI access. Revoke or rotate them in Agents & machines.
            </p>
          )}
        </Section>
      </div>

      <div className="min-w-0 space-y-3">
        <h3 className="text-sm font-semibold">
          {ci === 'shell' ? 'Run your command with Clavis' : 'Add this to your workflow'}
        </h3>
        <CopyBlock code={snippet} />
        <ul className="list-disc space-y-1 pl-4 text-xs text-muted marker:text-dim">
          {ci === 'forgejo' || ci === 'github' ? (
            <>
              <li>
                Each load step makes every key of the environment an environment variable for the
                next steps, under the same names everywhere:{' '}
                <code className="font-mono">DATABASE_URL</code>, not{' '}
                <code className="font-mono">PRODUCTION_DATABASE_URL</code>. Secret values are masked
                in the log.
              </li>
              {chosen.length > 1 && (
                <li>
                  There is one load step per environment; the <code className="font-mono">if:</code>{' '}
                  runs only the one for the pushed branch (or the environment picked in Run
                  workflow), with that environment’s token.
                </li>
              )}
              <li>
                It uses <code className="font-mono">clavis</code> when the runner image already has
                it, and otherwise downloads the CLI from GitHub releases (checksum verified).
              </li>
              {fingerprint && (
                <li>
                  This server uses a self-signed certificate, so the snippet includes its
                  fingerprint. The job then trusts exactly this server.
                </li>
              )}
            </>
          ) : ci === 'gitlab' ? (
            <li>One job per environment; each job gets its own token from a masked variable.</li>
          ) : (
            <li>
              <code className="font-mono">clavis run</code> starts your command with the
              environment’s values and writes nothing to disk.
            </li>
          )}
        </ul>
        <div className="flex items-center gap-2 pt-2">
          <Button variant="ghost" size="sm" onClick={onBack}>
            <ArrowLeft className="size-3.5" /> Back
          </Button>
          <Button
            variant={done ? 'primary' : 'secondary'}
            size="sm"
            className="ml-auto"
            onClick={onDone}
          >
            {done ? 'Done' : 'Close'}
          </Button>
        </div>
      </div>
    </div>
  )
}

function Section({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <section className="space-y-2.5">
      <h3 className="flex items-center gap-2 text-sm font-semibold">
        <span className="flex size-5 items-center justify-center rounded-full bg-elev font-mono text-3xs text-muted">
          {n}
        </span>
        {title}
      </h3>
      {children}
    </section>
  )
}
