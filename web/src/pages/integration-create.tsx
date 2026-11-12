import { Loader2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Field, Input, Notice, Select, Textarea } from '@/components/ui/field'
import { Modal, ModalFooter, ModalHeader } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'

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
}

export const providers: Provider[] = [
  {
    id: 'github',
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
    name: 'Forgejo Actions',
    blurb: 'Repository or organization secrets on Forgejo, Codeberg, or Gitea.',
    mark: 'FJ',
    color: '#fb923c',
    docs: 'Access token with write:repository (write:organization for org scope).',
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
        hint: 'owner/name for a repository; just the name for an organization.',
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
        <div className="mt-4 space-y-3">
          <Field
            label="Environment to push"
            hint="Values are resolved (references, inheritance) first."
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
          {visible.map((field) => (
            <Field
              key={field.key}
              label={field.label + (field.required ? '' : ' (optional)')}
              hint={field.hint}
            >
              {field.options ? (
                <Select
                  value={values[field.key] ?? ''}
                  onChange={(event) => {
                    setValues((state) => ({ ...state, [field.key]: event.target.value }))
                  }}
                >
                  {field.options.map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
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
                    setValues((state) => ({ ...state, [field.key]: event.target.value }))
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
                    setValues((state) => ({ ...state, [field.key]: event.target.value }))
                  }}
                />
              )}
            </Field>
          ))}
          <label className="flex items-start gap-2 text-sm text-muted">
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
          <p className="text-2xs text-dim">
            {provider.docs} Credentials are encrypted with the organization key and never shown
            again.
          </p>
          {target?.type === 'production' && (
            <Notice tone="warning">
              This pushes production values. The first run asks you to confirm a preview.
            </Notice>
          )}
          {error && <Notice>{error}</Notice>}
        </div>
        <ModalFooter>
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
