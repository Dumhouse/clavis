import { Loader2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Field, Input, Notice } from '@/components/ui/field'
import { Modal, ModalFooter, ModalHeader } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth, type Org } from '@/lib/auth'

/** Lowercase letters, digits and hyphens, as the server accepts. */
function slugify(name: string) {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
}

/**
 * Create an organization, or rename the current one. Renaming can change
 * the slug too; CLI configs (.clavis.yaml) that use the old slug then need
 * `clavis link`.
 */
export function OrgDialog({
  existing,
  onClose,
  onSaved,
}: {
  existing: Org | null
  onClose: () => void
  onSaved: (org: Org) => void
}) {
  const { orgs, setOrgs } = useAuth()
  const [name, setName] = useState(existing?.name ?? '')
  const [slug, setSlug] = useState(existing?.slug ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const nextSlug = slug.trim() || slugify(name)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const body = { name: name.trim(), slug: nextSlug }
    const result = existing
      ? await api.PATCH('/api/v1/orgs/{org}', {
          params: { path: { org: existing.slug } },
          body,
        })
      : await api.POST('/api/v1/orgs', { body })
    setBusy(false)
    if (result.error) {
      setError(
        apiErrorMessage(
          result.error,
          existing ? 'Could not rename the organization.' : 'Could not create the organization.',
        ),
      )
      return
    }
    const saved = existing
      ? { ...existing, name: result.data.name, slug: result.data.slug }
      : result.data
    setOrgs(
      existing
        ? orgs.map((candidate) => (candidate.id === existing.id ? saved : candidate))
        : [...orgs, saved],
    )
    onSaved(saved)
  }

  return (
    <Modal onClose={onClose} className="max-w-md">
      <form
        onSubmit={(event) => {
          void submit(event)
        }}
      >
        <ModalHeader
          title={existing ? `Rename ${existing.name}` : 'New organization'}
          description={
            existing
              ? 'Members, projects and secrets stay as they are.'
              : 'A separate space with its own members, projects and keys. You become its owner.'
          }
          onClose={onClose}
        />
        <div className="mt-4 space-y-3">
          <Field label="Name">
            <Input
              required
              autoFocus
              maxLength={100}
              value={name}
              placeholder="Acme"
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </Field>
          <Field
            label="Slug"
            hint={
              existing && nextSlug !== existing.slug
                ? `Used in URLs and the CLI. Directories linked to “${existing.slug}” need \`clavis link\` afterwards.`
                : 'Used in URLs and the CLI (clavis init --org …).'
            }
          >
            <Input
              mono
              value={slug}
              placeholder={slugify(name) || 'acme'}
              onChange={(event) => {
                setSlug(event.target.value)
              }}
            />
          </Field>
          {error && <Notice>{error}</Notice>}
        </div>
        <ModalFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" size="sm" disabled={busy || !name.trim()}>
            {busy && <Loader2 className="size-3.5 animate-spin" />}
            {existing ? 'Save' : 'Create organization'}
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  )
}
