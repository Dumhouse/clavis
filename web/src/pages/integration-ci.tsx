import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CloudDownload, Code2, Loader2, Plus } from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { CopyBlock } from '@/components/copy-command'
import { Button } from '@/components/ui/button'
import { Notice, Select } from '@/components/ui/field'
import { Modal, ModalHeader } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { ciSnippet, platforms, secretNameFor } from '@/lib/integration-guide'
import { useSelfSignedFingerprint } from '@/lib/server-certificate'
import { formatDate as formatDay, formatDateTime } from '@/lib/format'

type Identity = components['schemas']['IdentityBody']

function ago(value?: string) {
  if (!value) return 'never'
  return formatDateTime(value)
}

/**
 * Machine tokens that pipelines use to fetch secrets for this project
 * ("fetch when the job runs"). Creating, rotating and revoking them lives in
 * Agents & machines; this tab shows what each one can read and how to use it.
 */
export function CiAccessSection({
  orgSlug,
  projectId,
  onAdd,
}: {
  orgSlug: string
  projectId: string | undefined
  onAdd: () => void
}) {
  const [snippetFor, setSnippetFor] = useState<Identity | null>(null)
  const identities = useQuery({
    queryKey: ['identities', orgSlug],
    queryFn: async () => {
      const result = await api.GET('/api/v1/orgs/{org}/identities', {
        params: { path: { org: orgSlug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load tokens.'))
      return result.data?.identities ?? []
    },
  })
  const list = (identities.data ?? []).filter(
    (identity) => identity.type === 'machine' && identity.project_id === projectId,
  )

  return (
    <section className="rounded-app border border-border bg-surface">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <CloudDownload className="size-4 text-muted" />
        <h2 className="text-base font-semibold">CI access</h2>
        <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">{list.length}</span>
        <span className="hidden text-2xs text-dim md:inline">
          Jobs fetch secrets from Clavis when they run, with these tokens. Nothing is copied.
        </span>
        <Button variant="secondary" size="sm" className="ml-auto" onClick={onAdd}>
          <Plus className="size-3.5" /> Add CI access
        </Button>
      </div>
      <div className="divide-y divide-border-soft">
        {list.map((identity) => {
          const token = (identity.tokens ?? [])[0]
          return (
            <div key={identity.id} className="flex flex-wrap items-center gap-2 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="font-medium">{identity.name}</span>
                  <span className="text-dim">reads</span>
                  <span className="rounded-full bg-elev px-1.5 py-0.5 font-mono text-2xs">
                    {identity.environment || 'every environment'}
                  </span>
                  <span className="text-2xs text-dim">as {identity.role}</span>
                </p>
                <p className="text-2xs text-dim">
                  {token ? (
                    <>
                      <span className="font-mono">{token.prefix}</span> · last used{' '}
                      {ago(token.last_used_at)}
                      {token.expires_at && ` · expires ${formatDay(token.expires_at)}`}
                    </>
                  ) : (
                    'No active token'
                  )}
                </p>
              </div>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setSnippetFor(identity)
                }}
              >
                <Code2 className="size-3.5" /> Workflow
              </Button>
              <Link
                to="/identities"
                className="rounded-app-sm px-2 py-1 text-xs text-muted hover:bg-hover hover:text-text"
              >
                Rotate or revoke
              </Link>
            </div>
          )
        })}
        {identities.isLoading && (
          <p className="flex items-center gap-2 px-4 py-5 text-sm text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </p>
        )}
        {identities.error && <Notice className="m-4">{identities.error.message}</Notice>}
        {!identities.isLoading && list.length === 0 && (
          <div className="px-4 py-6 text-sm text-muted">
            <p>
              No pipeline fetches this project’s secrets yet. This is the recommended way for
              Forgejo, Gitea, GitHub and GitLab: the job loads the environment it deploys, under the
              same names, and every read is audited.
            </p>
            <Button variant="primary" size="sm" className="mt-3" onClick={onAdd}>
              Set up CI access
            </Button>
          </div>
        )}
      </div>
      {snippetFor && (
        <SnippetDialog
          identity={snippetFor}
          onClose={() => {
            setSnippetFor(null)
          }}
        />
      )}
    </section>
  )
}

function SnippetDialog({ identity, onClose }: { identity: Identity; onClose: () => void }) {
  const fingerprint = useSelfSignedFingerprint()
  const ciPlatforms = platforms.filter((platform) => platform.ci)
  const guess = ciPlatforms.find((platform) => identity.name.startsWith(`${platform.id}-`))
  const [platformId, setPlatformId] = useState(guess?.id ?? 'forgejo')
  const platform = ciPlatforms.find((candidate) => candidate.id === platformId) ?? ciPlatforms[0]
  const env = identity.environment || 'staging'
  const secret = secretNameFor(env, true)
  const snippet = ciSnippet({
    ci: platform?.ci ?? 'shell',
    server: window.location.origin,
    fingerprint,
    envs: [env],
    secretNames: { [env]: secret },
  })
  return (
    <Modal onClose={onClose} className="max-w-3xl">
      <ModalHeader
        title={`Use ${identity.name} in a workflow`}
        description={`Loads the ${env} environment. The token itself is in your CI as ${secret}; Clavis can't show it again (rotate it in Agents & machines if it's lost).`}
        onClose={onClose}
      />
      <div className="mt-4 space-y-3">
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted">CI</span>
          <Select
            className="max-w-56"
            value={platformId}
            onChange={(event) => {
              setPlatformId(event.target.value)
            }}
          >
            {ciPlatforms.map((candidate) => (
              <option key={candidate.id} value={candidate.id}>
                {candidate.name}
              </option>
            ))}
          </Select>
        </label>
        <CopyBlock code={snippet} />
      </div>
    </Modal>
  )
}
