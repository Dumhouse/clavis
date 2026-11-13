import { ExternalLink, KeyRound, Loader2, Lock } from 'lucide-react'
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

type FieldSpec = {
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
  const target = envs.find((candidate) => candidate.slug === env)
  const visible = provider.fields.filter((field) => !field.showIf || field.showIf(values))

  const destination = visible.filter((field) => !field.secret)
  const credentials = visible.filter((field) => field.secret)
  const tokenLink = provider.tokenUrl?.(values)
  const where = destination
    .filter((field) => !field.options)
    .map((field) => values[field.key]?.trim())
    .find(Boolean)
  const summary = `${env || '…'} → ${provider.name}${where ? ` · ${where}` : ''}`

  function renderField(field: FieldSpec) {
    const set = (value: string) => {
      setValues((state) => ({ ...state, [field.key]: value }))
    }
    const label = field.label + (field.required ? '' : ' (optional)')
    if (field.options && field.options.length <= 3) {
      return (
        <fieldset key={field.key}>
          <legend className="mb-1.5 text-sm font-medium">{label}</legend>
          <div
            className="grid gap-1.5"
            style={{
              gridTemplateColumns: `repeat(${String(field.options.length)}, minmax(0, 1fr))`,
            }}
          >
            {field.options.map(([value, optionLabel]) => {
              const checked = (values[field.key] ?? '') === value
              return (
                <label
                  key={value}
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
                    value={value}
                    checked={checked}
                    onChange={() => {
                      set(value)
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
      <Field key={field.key} label={label} hint={field.hint}>
        {field.options ? (
          <Select
            value={values[field.key] ?? ''}
            onChange={(event) => {
              set(event.target.value)
            }}
          >
            {field.options.map(([value, optionLabel]) => (
              <option key={value} value={value}>
                {optionLabel}
              </option>
            ))}
          </Select>
        ) : field.multiline ? (
          <Textarea
            mono
            rows={4}
            required={field.required}
            autoComplete="off"
            spellCheck={false}
            placeholder={field.placeholder}
            value={values[field.key] ?? ''}
            onChange={(event) => {
              set(event.target.value)
            }}
          />
        ) : (
          <Input
            type={field.secret ? 'password' : 'text'}
            mono={field.secret === true}
            required={field.required}
            autoComplete={field.secret ? 'new-password' : 'off'}
            spellCheck={false}
            placeholder={field.placeholder}
            value={values[field.key] ?? ''}
            onChange={(event) => {
              set(event.target.value)
            }}
          />
        )}
      </Field>
    )
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
    for (const field of visible) {
      const value = values[field.key]?.trim()
      if (!value) continue
      body[field.key] = field.key === 'vault_version' ? Number(value) : value
    }
    const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/syncs', {
      params: { path: { org: orgSlug, project: projectSlug } },
      body: body as Body,
    })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not create the sync.'))
      return
    }
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
            {destination.map(renderField)}
          </FormSection>

          {credentials.length > 0 && (
            <FormSection step={3} title="Credentials">
              <div className="rounded-app-sm border border-border-soft bg-panel px-3 py-2.5 text-xs leading-relaxed text-muted">
                <p className="flex items-start gap-1.5">
                  <KeyRound className="mt-0.5 size-3.5 shrink-0 text-dim" />
                  <span>{provider.docs}</span>
                </p>
                {tokenLink && (
                  <a
                    href={tokenLink}
                    target="_blank"
                    rel="noreferrer"
                    className="mt-1.5 inline-flex items-center gap-1 font-medium text-accent-text hover:underline"
                  >
                    Create a token <ExternalLink className="size-3" />
                  </a>
                )}
              </div>
              {credentials.map(renderField)}
              <p className="flex items-center gap-1.5 text-2xs text-dim">
                <Lock className="size-3" /> Encrypted with the organization key and never shown
                again.
              </p>
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
