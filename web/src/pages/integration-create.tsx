import { ExternalLink, KeyRound, Loader2, Lock } from 'lucide-react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent, type ReactNode } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Field, Input, Notice, Select, Textarea } from '@/components/ui/field'
import { Modal, ModalFooter, ModalHeader } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Body = components['schemas']['CreateSyncInputBody']
type BodyKey = Exclude<keyof Body, 'environment' | 'provider'>

export type FieldSpec = {
  key: BodyKey
  label: string
  placeholder?: string
  hint?: string
  secret?: boolean
  multiline?: boolean
  required?: boolean
  options?: Array<[string, string]>
  showIf?: (values: Record<string, string>) => boolean
}

export type Provider = {
  id: string
  name: string
  blurb: string
  mark: string
  color: string
  fields: FieldSpec[]
  docs: string
  /** Where to create the credential, given the form values (e.g. the instance URL). */
  tokenUrl?: (values: Record<string, string>) => string
}

/**
 * Fields that belong to the credential rather than the destination. For every
 * provider except HTTP they can live in a reusable org connection.
 */
const credentialKeys = new Set<BodyKey>([
  'base_url',
  'token',
  'aws_access_key',
  'aws_secret_key',
  'aws_session_token',
  'gcp_service_account',
])

export function supportsConnections(provider: Provider) {
  return provider.id !== 'http'
}

/** The fields a connection for this provider stores. */
export function connectionFields(provider: Provider) {
  return supportsConnections(provider)
    ? provider.fields.filter((field) => credentialKeys.has(field.key))
    : []
}

/** A readable default name, e.g. "Forgejo Actions · git.example.com". */
export function defaultConnectionName(provider: Provider, values: Record<string, string>) {
  const raw = values.base_url?.trim()
  let host = ''
  if (raw) {
    try {
      host = new URL(raw).host
    } catch {
      host = raw
    }
  }
  return host ? `${provider.name} · ${host}` : provider.name
}

export const providers: Provider[] = [
  {
    id: 'github',
    tokenUrl: () => 'https://github.com/settings/personal-access-tokens/new',
    name: 'GitHub Actions',
    blurb: 'Repository, environment, or organization secrets.',
    mark: 'GH',
    color: '#8b949e',
    docs: 'Fine-grained token with Secrets: write, or a GitHub App.',
    fields: [
      {
        key: 'github_scope',
        label: 'Scope',
        options: [
          ['repo', 'Repository secrets'],
          ['environment', 'Environment secrets'],
          ['org', 'Organization secrets'],
        ],
      },
      {
        key: 'github_repo',
        label: 'Repository or organization',
        placeholder: 'acme/web',
        required: true,
      },
      {
        key: 'github_environment',
        label: 'GitHub environment',
        placeholder: 'production',
        showIf: (values) => values.github_scope === 'environment',
        required: true,
      },
      {
        key: 'base_url',
        label: 'API URL',
        placeholder: 'https://api.github.com',
        hint: 'Only for GitHub Enterprise Server.',
      },
      { key: 'token', label: 'Access token', secret: true, required: true },
    ],
  },
  {
    id: 'gitlab',
    tokenUrl: (values) =>
      `${(values.base_url?.trim() || 'https://gitlab.com').replace(/\/+$/, '')}/-/user_settings/personal_access_tokens`,
    name: 'GitLab CI',
    blurb: 'Project CI/CD variables, masked and protected.',
    mark: 'GL',
    color: '#fc6d26',
    docs: 'Project access token with the api scope.',
    fields: [
      {
        key: 'gitlab_project',
        label: 'Project path or ID',
        placeholder: 'acme/web',
        required: true,
      },
      { key: 'base_url', label: 'GitLab URL', placeholder: 'https://gitlab.com' },
      { key: 'token', label: 'Access token', secret: true, required: true },
    ],
  },
  {
    id: 'forgejo',
    tokenUrl: (values) =>
      `${(values.base_url?.trim() || 'https://codeberg.org').replace(/\/+$/, '')}/user/settings/applications`,
    name: 'Forgejo Actions',
    blurb: 'Repository or organization secrets on Forgejo, Codeberg, or Gitea.',
    mark: 'FJ',
    color: '#fb923c',
    docs: 'In Forgejo: avatar → Settings → Applications → Generate New Token. Set “repository” to Read and Write (“organization” to Read and Write for org scope). Codeberg: codeberg.org/user/settings/applications.',
    fields: [
      {
        key: 'forgejo_scope',
        label: 'Scope',
        options: [
          ['repo', 'Repository secrets'],
          ['org', 'Organization secrets'],
        ],
      },
      {
        key: 'forgejo_repo',
        label: 'Repository or organization',
        placeholder: 'team/web',
        hint: 'Where the secrets will appear: owner/name for a repository (team/web), or just the name for an organization.',
        required: true,
      },
      {
        key: 'base_url',
        label: 'Forgejo URL',
        placeholder: 'https://codeberg.org',
        hint: 'Your instance, e.g. https://git.example.com. Gitea works too.',
      },
      { key: 'token', label: 'Access token', secret: true, required: true },
    ],
  },
  {
    id: 'vercel',
    tokenUrl: () => 'https://vercel.com/account/settings/tokens',
    name: 'Vercel',
    blurb: 'Project environment variables per target.',
    mark: '▲',
    color: '#ededed',
    docs: 'Account token scoped to the team that owns the project.',
    fields: [
      { key: 'vercel_project', label: 'Project ID or name', placeholder: 'web', required: true },
      { key: 'vercel_team_id', label: 'Team ID', placeholder: 'team_…' },
      { key: 'token', label: 'Access token', secret: true, required: true },
    ],
  },
  {
    id: 'netlify',
    tokenUrl: () => 'https://app.netlify.com/user/applications#personal-access-tokens',
    name: 'Netlify',
    blurb: 'Site or account environment variables.',
    mark: 'N',
    color: '#32e6e2',
    docs: 'Personal access token.',
    fields: [
      { key: 'netlify_account', label: 'Account slug', placeholder: 'acme', required: true },
      { key: 'netlify_site', label: 'Site ID', placeholder: 'optional' },
      { key: 'token', label: 'Access token', secret: true, required: true },
    ],
  },
  {
    id: 'cloudflare',
    tokenUrl: () => 'https://dash.cloudflare.com/profile/api-tokens',
    name: 'Cloudflare',
    blurb: 'Workers secrets or Pages environment variables.',
    mark: 'CF',
    color: '#f38020',
    docs: 'API token with Workers Scripts or Pages edit permission.',
    fields: [
      {
        key: 'cloudflare_mode',
        label: 'Target',
        options: [
          ['workers', 'Workers'],
          ['pages', 'Pages'],
        ],
      },
      { key: 'cloudflare_account', label: 'Account ID', required: true },
      {
        key: 'cloudflare_script',
        label: 'Worker script',
        showIf: (values) => values.cloudflare_mode !== 'pages',
        required: true,
      },
      {
        key: 'cloudflare_project',
        label: 'Pages project',
        showIf: (values) => values.cloudflare_mode === 'pages',
        required: true,
      },
      { key: 'token', label: 'API token', secret: true, required: true },
    ],
  },
  {
    id: 'aws',
    name: 'AWS',
    blurb: 'SSM Parameter Store or Secrets Manager.',
    mark: 'AWS',
    color: '#ff9900',
    docs: 'IAM user or role keys allowed to write the chosen service.',
    fields: [
      {
        key: 'aws_service',
        label: 'Service',
        options: [
          ['ssm', 'SSM Parameter Store'],
          ['secretsmanager', 'Secrets Manager'],
        ],
      },
      { key: 'aws_region', label: 'Region', placeholder: 'eu-west-1', required: true },
      { key: 'aws_prefix', label: 'Path prefix', placeholder: 'acme/web/prod' },
      { key: 'aws_access_key', label: 'Access key ID', required: true },
      { key: 'aws_secret_key', label: 'Secret access key', secret: true, required: true },
      { key: 'aws_session_token', label: 'Session token', secret: true },
    ],
  },
  {
    id: 'gcp',
    name: 'Google Cloud',
    blurb: 'Secret Manager secrets.',
    mark: 'GCP',
    color: '#4285f4',
    docs: 'Service account with Secret Manager Admin on the project.',
    fields: [
      { key: 'gcp_project', label: 'Project ID', placeholder: 'acme-prod', required: true },
      { key: 'gcp_prefix', label: 'Name prefix', placeholder: 'web' },
      {
        key: 'gcp_service_account',
        label: 'Service account JSON',
        secret: true,
        multiline: true,
        required: true,
      },
    ],
  },
  {
    id: 'vault',
    name: 'HashiCorp Vault',
    blurb: 'KV v1 or v2 mount, for teams migrating gradually.',
    mark: 'V',
    color: '#ffd814',
    docs: 'Token with write access to the path.',
    fields: [
      {
        key: 'base_url',
        label: 'Vault address',
        placeholder: 'https://vault.acme.dev',
        required: true,
      },
      { key: 'vault_mount', label: 'Mount', placeholder: 'secret' },
      { key: 'vault_path', label: 'Path', placeholder: 'web/prod' },
      {
        key: 'vault_version',
        label: 'KV version',
        options: [
          ['2', 'KV v2'],
          ['1', 'KV v1'],
        ],
      },
      { key: 'token', label: 'Token', secret: true, required: true },
    ],
  },
  {
    id: 'http',
    name: 'Webhook (HTTP)',
    blurb: 'POST resolved values to your own endpoint, HMAC-signed.',
    mark: '{ }',
    color: '#a78bfa',
    docs: 'Verify the X-Clavis-Signature header with the signing secret.',
    fields: [
      {
        key: 'endpoint',
        label: 'Endpoint URL',
        placeholder: 'https://deploy.acme.dev/hooks/env',
        required: true,
      },
      { key: 'secret', label: 'Signing secret', secret: true },
    ],
  },
]

export function ProviderMark({
  provider,
  size = 'md',
}: {
  provider: Provider
  size?: 'sm' | 'md'
}) {
  return (
    <span
      aria-hidden
      className={
        size === 'sm'
          ? 'flex size-6 shrink-0 items-center justify-center rounded-md bg-elev font-mono text-3xs font-bold'
          : 'flex size-9 shrink-0 items-center justify-center rounded-app-sm bg-elev font-mono text-2xs font-bold'
      }
      style={{ color: provider.color }}
    >
      {provider.mark}
    </span>
  )
}

/** One provider field: a segmented choice, select, textarea, or input. */
export function SpecField({
  field,
  value,
  onChange,
  required = field.required,
  placeholder = field.placeholder,
  labelSuffix,
}: {
  field: FieldSpec
  value: string
  onChange: (value: string) => void
  required?: boolean | undefined
  placeholder?: string | undefined
  labelSuffix?: string
}) {
  const label = field.label + (labelSuffix ?? (required ? '' : ' (optional)'))
  if (field.options && field.options.length <= 3) {
    return (
      <fieldset>
        <legend className="mb-1.5 text-sm font-medium">{label}</legend>
        <div
          className="grid gap-1.5"
          style={{
            gridTemplateColumns: `repeat(${String(field.options.length)}, minmax(0, 1fr))`,
          }}
        >
          {field.options.map(([optionValue, optionLabel]) => {
            const checked = value === optionValue
            return (
              <label
                key={optionValue}
                className={cn(
                  'flex cursor-pointer items-center justify-center rounded-app-sm border px-2 py-1.5 text-center text-xs transition-colors',
                  checked
                    ? 'border-accent-line bg-accent-soft font-medium text-accent-text'
                    : 'border-border text-muted hover:bg-hover hover:text-text',
                )}
              >
                <input
                  type="radio"
                  className="sr-only"
                  name={field.key}
                  value={optionValue}
                  checked={checked}
                  onChange={() => {
                    onChange(optionValue)
                  }}
                />
                {optionLabel}
              </label>
            )
          })}
        </div>
        {field.hint && <p className="mt-1 text-2xs text-dim">{field.hint}</p>}
      </fieldset>
    )
  }
  return (
    <Field label={label} hint={field.hint}>
      {field.options ? (
        <Select
          value={value}
          onChange={(event) => {
            onChange(event.target.value)
          }}
        >
          {field.options.map(([optionValue, optionLabel]) => (
            <option key={optionValue} value={optionValue}>
              {optionLabel}
            </option>
          ))}
        </Select>
      ) : field.multiline ? (
        <Textarea
          mono
          rows={4}
          required={required}
          autoComplete="off"
          spellCheck={false}
          placeholder={placeholder}
          value={value}
          onChange={(event) => {
            onChange(event.target.value)
          }}
        />
      ) : (
        <Input
          type={field.secret ? 'password' : 'text'}
          mono={field.secret === true}
          required={required}
          autoComplete={field.secret ? 'new-password' : 'off'}
          spellCheck={false}
          placeholder={placeholder}
          value={value}
          onChange={(event) => {
            onChange(event.target.value)
          }}
        />
      )}
    </Field>
  )
}

/** Where to create a token, with the provider's scope advice. */
export function TokenHelp({ provider, link }: { provider: Provider; link?: string | undefined }) {
  return (
    <div className="rounded-app-sm border border-border-soft bg-panel px-3 py-2.5 text-xs leading-relaxed text-muted">
      <p className="flex items-start gap-1.5">
        <KeyRound className="mt-0.5 size-3.5 shrink-0 text-dim" />
        <span>{provider.docs}</span>
      </p>
      {link && (
        <a
          href={link}
          target="_blank"
          rel="noreferrer"
          className="mt-1.5 inline-flex items-center gap-1 font-medium text-accent-text hover:underline"
        >
          Create a token <ExternalLink className="size-3" />
        </a>
      )}
    </div>
  )
}

const NEW_CONNECTION = 'new'

export function CreateSyncDialog({
  orgSlug,
  projectSlug,
  envs,
  provider,
  onClose,
  onCreated,
}: {
  orgSlug: string
  projectSlug: string
  envs: Env[]
  provider: Provider
  onClose: () => void
  onCreated: () => void
}) {
  const queryClient = useQueryClient()
  const [env, setEnv] = useState(envs[0]?.slug ?? '')
  const [values, setValues] = useState<Record<string, string>>(() => {
    const initial: Record<string, string> = {}
    for (const field of provider.fields) {
      if (field.options?.[0]) initial[field.key] = field.options[0][0]
    }
    return initial
  })
  const [deleteMissing, setDeleteMissing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Connection choice: an existing connection ID, or NEW_CONNECTION. Unset
  // means "the first saved connection, if any".
  const [picked, setPicked] = useState<string | null>(null)
  const [connectionName, setConnectionName] = useState('')
  const [saveConnection, setSaveConnection] = useState(true)

  const shared = supportsConnections(provider)
  const connections = useQuery({
    queryKey: ['connections', orgSlug, provider.id],
    enabled: shared,
    queryFn: async () => {
      const result = await api.GET('/api/v1/orgs/{org}/connections', {
        params: { path: { org: orgSlug }, query: { provider: provider.id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load connections.'))
      return result.data?.connections ?? []
    },
  })
  const saved = connections.data ?? []
  const choice = picked ?? saved[0]?.id ?? NEW_CONNECTION
  const chosen = saved.find((connection) => connection.id === choice)

  const target = envs.find((candidate) => candidate.slug === env)
  const visible = provider.fields.filter((field) => !field.showIf || field.showIf(values))
  const credentialSet = new Set(connectionFields(provider).map((field) => field.key))
  const destination = visible.filter((field) =>
    shared ? !credentialSet.has(field.key) : !field.secret,
  )
  const credentials = visible.filter((field) =>
    shared ? credentialSet.has(field.key) : field.secret,
  )
  const tokenLink = provider.tokenUrl?.(chosen ? { ...values, base_url: chosen.base_url } : values)
  const where = destination
    .filter((field) => !field.options)
    .map((field) => values[field.key]?.trim())
    .find(Boolean)
  const summary = `${env || '…'} → ${provider.name}${where ? ` · ${where}` : ''}`
  const set = (key: string) => (value: string) => {
    setValues((state) => ({ ...state, [key]: value }))
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const body: Record<string, unknown> = {
      provider: provider.id,
      environment: env,
      delete_missing: deleteMissing,
    }
    const fill = (fields: FieldSpec[], into: Record<string, unknown>) => {
      for (const field of fields) {
        const value = values[field.key]?.trim()
        if (!value) continue
        into[field.key] = field.key === 'vault_version' ? Number(value) : value
      }
    }
    fill(destination, body)
    let connectionID = chosen?.id
    if (shared && !chosen && saveConnection) {
      const connectionBody: Record<string, unknown> = {
        provider: provider.id,
        name: connectionName.trim() || defaultConnectionName(provider, values),
      }
      fill(credentials, connectionBody)
      const created = await api.POST('/api/v1/orgs/{org}/connections', {
        params: { path: { org: orgSlug } },
        body: connectionBody as components['schemas']['CreateConnectionInputBody'],
      })
      if (created.error) {
        setBusy(false)
        setError(apiErrorMessage(created.error, 'Could not save the connection.'))
        return
      }
      connectionID = created.data.id
      // A retry after a failed sync reuses the connection just saved.
      setPicked(connectionID)
      await queryClient.invalidateQueries({ queryKey: ['connections', orgSlug] })
    }
    if (connectionID) body.connection_id = connectionID
    else fill(credentials, body)
    const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/syncs', {
      params: { path: { org: orgSlug, project: projectSlug } },
      body: body as Body,
    })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not create the sync.'))
      return
    }
    await queryClient.invalidateQueries({ queryKey: ['connections', orgSlug] })
    onCreated()
  }

  return (
    <Modal onClose={onClose} className="max-w-lg">
      <form
        onSubmit={(event) => {
          void submit(event)
        }}
      >
        <ModalHeader
          title={
            <span className="flex items-center gap-2.5">
              <ProviderMark provider={provider} size="sm" /> Sync to {provider.name}
            </span>
          }
          description={provider.blurb}
          onClose={onClose}
        />
        <div className="mt-5 space-y-5">
          <FormSection step={1} title="What to push">
            <Field
              label="Clavis environment"
              hint="One sync pushes one environment to one destination. Add another sync for staging or production."
            >
              <Select
                value={env}
                onChange={(event) => {
                  setEnv(event.target.value)
                }}
              >
                {envs.map((candidate) => (
                  <option key={candidate.id} value={candidate.slug}>
                    {candidate.name} ({candidate.slug})
                  </option>
                ))}
              </Select>
            </Field>
            {target?.type === 'production' && (
              <Notice tone="warning">
                This pushes production values. The first run asks you to confirm a preview.
              </Notice>
            )}
          </FormSection>

          <FormSection step={2} title={`Where in ${provider.name}`}>
            {destination.map((field) => (
              <SpecField
                key={field.key}
                field={field}
                value={values[field.key] ?? ''}
                onChange={set(field.key)}
              />
            ))}
          </FormSection>

          {credentials.length > 0 && (
            <FormSection step={3} title={shared ? 'Connection' : 'Credentials'}>
              {shared && (
                <fieldset className="space-y-1.5">
                  <legend className="sr-only">Connection</legend>
                  {saved.map((connection) => (
                    <ConnectionChoice
                      key={connection.id}
                      checked={choice === connection.id}
                      onSelect={() => {
                        setPicked(connection.id)
                      }}
                      title={connection.name}
                      detail={[
                        connection.base_url,
                        connection.sync_count === 1
                          ? 'used by 1 sync'
                          : `used by ${String(connection.sync_count)} syncs`,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    />
                  ))}
                  <ConnectionChoice
                    checked={choice === NEW_CONNECTION}
                    onSelect={() => {
                      setPicked(NEW_CONNECTION)
                    }}
                    title={saved.length > 0 ? 'Add another connection' : 'Add a connection'}
                    detail={`Enter a ${provider.name} credential once; later syncs just pick it.`}
                  />
                  {connections.isLoading && (
                    <p className="flex items-center gap-1.5 text-2xs text-dim">
                      <Loader2 className="size-3 animate-spin" /> Loading saved connections…
                    </p>
                  )}
                </fieldset>
              )}
              {!chosen && (
                <>
                  <TokenHelp provider={provider} link={tokenLink} />
                  {credentials.map((field) => (
                    <SpecField
                      key={field.key}
                      field={field}
                      value={values[field.key] ?? ''}
                      onChange={set(field.key)}
                    />
                  ))}
                  {shared && (
                    <div className="space-y-2 rounded-app-sm border border-border-soft px-3 py-2.5">
                      <label className="flex cursor-pointer items-start gap-2.5 text-sm">
                        <input
                          type="checkbox"
                          className="mt-0.5"
                          checked={saveConnection}
                          onChange={(event) => {
                            setSaveConnection(event.target.checked)
                          }}
                        />
                        <span>
                          Save as a connection for other syncs
                          <span className="block text-2xs text-dim">
                            Needs org-wide sync permission. Off stores the credential on this sync
                            only.
                          </span>
                        </span>
                      </label>
                      {saveConnection && (
                        <Field label="Connection name">
                          <Input
                            value={connectionName}
                            placeholder={defaultConnectionName(provider, values)}
                            maxLength={100}
                            onChange={(event) => {
                              setConnectionName(event.target.value)
                            }}
                          />
                        </Field>
                      )}
                    </div>
                  )}
                  <p className="flex items-center gap-1.5 text-2xs text-dim">
                    <Lock className="size-3" /> Encrypted with the organization key and never shown
                    again.
                  </p>
                </>
              )}
            </FormSection>
          )}

          <FormSection step={credentials.length > 0 ? 4 : 3} title="Options">
            <label className="flex cursor-pointer items-start gap-2.5 rounded-app-sm border border-border-soft px-3 py-2.5 text-sm hover:bg-hover">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={deleteMissing}
                onChange={(event) => {
                  setDeleteMissing(event.target.checked)
                }}
              />
              <span>
                Delete keys in {provider.name} that are not in Clavis
                <span className="block text-2xs text-dim">Off is safer for a first sync.</span>
              </span>
            </label>
          </FormSection>

          {error && <Notice>{error}</Notice>}
        </div>
        <ModalFooter className="mt-6 items-center border-t border-border-soft pt-4">
          <p className="mr-auto min-w-0 truncate font-mono text-2xs text-dim" title={summary}>
            {summary}
          </p>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" size="sm" disabled={busy || !env}>
            {busy && <Loader2 className="size-3.5 animate-spin" />} Create sync
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  )
}

function ConnectionChoice({
  checked,
  onSelect,
  title,
  detail,
}: {
  checked: boolean
  onSelect: () => void
  title: string
  detail: string
}) {
  return (
    <label
      className={cn(
        'flex cursor-pointer items-start gap-2.5 rounded-app-sm border px-3 py-2 text-sm transition-colors',
        checked ? 'border-accent-line bg-accent-soft' : 'border-border-soft hover:bg-hover',
      )}
    >
      <input
        type="radio"
        name="connection"
        className="mt-1"
        checked={checked}
        onChange={onSelect}
      />
      <span className="min-w-0">
        <span className="block font-medium">{title}</span>
        <span className="block truncate text-2xs text-dim">{detail}</span>
      </span>
    </label>
  )
}

function FormSection({
  step,
  title,
  children,
}: {
  step: number
  title: string
  children: ReactNode
}) {
  return (
    <section className="space-y-3">
      <h3 className="flex items-center gap-2 text-2xs font-semibold uppercase tracking-[0.06em] text-dim">
        <span className="flex size-4 items-center justify-center rounded-full bg-elev font-mono text-3xs text-muted">
          {step}
        </span>
        {title}
      </h3>
      {children}
    </section>
  )
}
