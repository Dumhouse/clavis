import { useMutation } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { LogOut, Trash2 } from 'lucide-react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { confirmAction, promptDialog } from '@/components/ui/dialogs'
import { Notice } from '@/components/ui/field'
import { toast } from '@/components/ui/toast'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth } from '@/lib/auth'
import { useCurrentOrg } from '@/lib/org'
import { ensureReauth } from '@/lib/reauth'

/**
 * Leave the organization (anyone but its last owner) or delete it (owners).
 * Both ask for your password again, and deleting asks for the name.
 */
export function OrgDangerZone() {
  const { org, orgs } = useCurrentOrg()
  const { setOrgs } = useAuth()
  const navigate = useNavigate()

  const done = (message: string) => {
    if (!org) return
    setOrgs(orgs.filter((candidate) => candidate.slug !== org.slug))
    toast(message)
    void navigate({ to: '/' })
  }

  const leave = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const ok = await confirmAction(
        `Leave ${org.name}? You lose access to its projects. An admin can invite you again.`,
      )
      if (!ok) return false
      const result = await api.POST('/api/v1/orgs/{org}/leave', {
        params: { path: { org: org.slug } },
        headers: { 'X-Clavis-Reauth': await ensureReauth() },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not leave.'))
      return true
    },
    onSuccess: (left) => {
      if (left) done(`You left ${org?.name ?? 'the organization'}.`)
    },
  })

  const remove = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const typed = await promptDialog({
        title: `Delete ${org.name}?`,
        body: 'This deletes every project, secret, token and audit event. It cannot be undone.',
        label: `Type ${org.slug} to confirm`,
        confirmLabel: 'Delete organization',
        production: true,
      })
      if (typed?.trim() !== org.slug) return false
      const result = await api.DELETE('/api/v1/orgs/{org}', {
        params: { path: { org: org.slug } },
        headers: { 'X-Clavis-Reauth': await ensureReauth() },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not delete.'))
      return true
    },
    onSuccess: (deleted) => {
      if (deleted) done(`Deleted ${org?.name ?? 'the organization'}.`)
    },
  })

  if (!org) return null
  const error = leave.error ?? remove.error
  return (
    <section
      id="danger"
      className="scroll-mt-4 rounded-app border border-danger/40 bg-surface"
      aria-labelledby="danger-heading"
    >
      <div className="border-b border-danger/30 px-4 py-3">
        <h2 id="danger-heading" className="text-base font-semibold text-danger">
          Leave or delete
        </h2>
      </div>
      <div className="divide-y divide-border-soft">
        <div className="flex flex-wrap items-center gap-3 px-4 py-3">
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium">Leave {org.name}</p>
            <p className="text-xs text-muted">
              You lose access to its projects. The last owner can&apos;t leave: hand ownership to
              someone else or delete the organization.
            </p>
          </div>
          <Button
            variant="secondary"
            size="sm"
            disabled={leave.isPending}
            onClick={() => {
              leave.mutate()
            }}
          >
            <LogOut className="size-3.5" /> Leave organization
          </Button>
        </div>
        {org.role === 'Owner' && (
          <div className="flex flex-wrap items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">Delete {org.name}</p>
              <p className="text-xs text-muted">
                Removes every project, secret, token and audit event for everyone. This can&apos;t
                be undone; export or back up first.
              </p>
            </div>
            <Button
              variant="danger"
              size="sm"
              disabled={remove.isPending}
              onClick={() => {
                remove.mutate()
              }}
            >
              <Trash2 className="size-3.5" /> Delete organization
            </Button>
          </div>
        )}
      </div>
      {error && (
        <div className="p-3">
          <Notice>{error.message}</Notice>
        </div>
      )}
    </section>
  )
}
