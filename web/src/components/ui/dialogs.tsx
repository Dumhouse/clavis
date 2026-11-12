import { AlertTriangle, Loader2 } from 'lucide-react'
import { useState, useSyncExternalStore, type FormEvent, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Field, Input, Textarea } from '@/components/ui/field'
import { Modal, ModalFooter, ModalHeader } from '@/components/ui/modal'

/*
 * Promise-based replacements for window.confirm / window.prompt. They work
 * from any module (including non-React helpers such as reauth) and render
 * through <DialogHost/>, which main.tsx mounts once.
 */

type ConfirmOptions = {
  title: string
  body?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
}

type PromptOptions = {
  title: string
  body?: ReactNode
  label: string
  placeholder?: string
  confirmLabel?: string
  /** 'password' masks input and enables password managers. */
  type?: 'text' | 'password' | 'reason'
  /** Suggested one-click answers (for reasons). */
  suggestions?: string[]
  /** Highlight that this acts on production data. */
  production?: boolean
  minLength?: number
}

type Request =
  | ({ kind: 'confirm'; id: number; resolve: (value: boolean) => void } & ConfirmOptions)
  | ({ kind: 'prompt'; id: number; resolve: (value: string | null) => void } & PromptOptions)

let queue: Request[] = []
let nextId = 1
const listeners = new Set<() => void>()

function emit() {
  for (const listener of listeners) listener()
}

function push(request: Request) {
  queue = [...queue, request]
  emit()
}

function finish(id: number) {
  queue = queue.filter((request) => request.id !== id)
  emit()
}

export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    push({ kind: 'confirm', id: nextId++, resolve, ...options })
  })
}

export function promptDialog(options: PromptOptions): Promise<string | null> {
  return new Promise((resolve) => {
    push({ kind: 'prompt', id: nextId++, resolve, ...options })
  })
}

/**
 * Confirm a destructive action. A message like "Delete X? Details." becomes a
 * title ("Delete X?") and a body ("Details.").
 */
export function confirmAction(message: string, confirmLabel?: string): Promise<boolean> {
  const split = message.indexOf('? ')
  const title = split === -1 ? message : message.slice(0, split + 1)
  const body = split === -1 ? undefined : message.slice(split + 2)
  const verb = /^(\w+)/.exec(title)?.[1]
  return confirmDialog({
    title,
    ...(body ? { body } : {}),
    danger: true,
    confirmLabel: confirmLabel ?? verb ?? 'Confirm',
  })
}

/** Ask why a value is being revealed. Resolves null when cancelled. */
export function askRevealReason(target: string, production: boolean): Promise<string | null> {
  return promptDialog({
    title: production ? 'Reveal a production value' : 'Reveal value',
    body: (
      <>
        <span className="font-mono text-text">{target}</span> will be shown in plain text. The
        reason is written to the audit log; the value never is.
      </>
    ),
    label: 'Reason',
    placeholder: 'e.g. debugging failed checkout, INC-1423',
    confirmLabel: 'Reveal',
    type: 'reason',
    production,
    minLength: 3,
    suggestions: ['Debugging an incident', 'Verifying a rotation', 'Configuring a new service'],
  })
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function DialogHost() {
  const requests = useSyncExternalStore(
    subscribe,
    () => queue,
    () => queue,
  )
  const current = requests[0]
  if (!current) return null
  if (current.kind === 'confirm') {
    return (
      <ConfirmModal
        key={current.id}
        request={current}
        onDone={(value) => {
          current.resolve(value)
          finish(current.id)
        }}
      />
    )
  }
  return (
    <PromptModal
      key={current.id}
      request={current}
      onDone={(value) => {
        current.resolve(value)
        finish(current.id)
      }}
    />
  )
}

function ConfirmModal({
  request,
  onDone,
}: {
  request: ConfirmOptions
  onDone: (value: boolean) => void
}) {
  return (
    <Modal
      onClose={() => {
        onDone(false)
      }}
      className="max-w-md"
    >
      <div className="flex gap-3">
        {request.danger && (
          <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-danger-bg text-danger">
            <AlertTriangle className="size-4" />
          </span>
        )}
        <div className="min-w-0">
          <h2 className="font-head text-md font-semibold">{request.title}</h2>
          {request.body && <div className="mt-1 text-sm text-muted">{request.body}</div>}
        </div>
      </div>
      <ModalFooter>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            onDone(false)
          }}
        >
          {request.cancelLabel ?? 'Cancel'}
        </Button>
        <Button
          autoFocus
          variant={request.danger ? 'danger-solid' : 'primary'}
          size="sm"
          onClick={() => {
            onDone(true)
          }}
        >
          {request.confirmLabel ?? 'Confirm'}
        </Button>
      </ModalFooter>
    </Modal>
  )
}

function PromptModal({
  request,
  onDone,
}: {
  request: PromptOptions
  onDone: (value: string | null) => void
}) {
  const [value, setValue] = useState('')
  const [busy, setBusy] = useState(false)
  const min = request.minLength ?? 1
  const valid = value.trim().length >= min

  function submit(event: FormEvent) {
    event.preventDefault()
    if (!valid) return
    setBusy(true)
    onDone(request.type === 'password' ? value : value.trim())
  }

  return (
    <Modal
      onClose={() => {
        onDone(null)
      }}
      className="max-w-md"
    >
      <form onSubmit={submit}>
        <ModalHeader
          title={
            <span className="flex items-center gap-2">
              {request.title}
              {request.production && <span className="env-chip env-production">prod</span>}
            </span>
          }
          onClose={() => {
            onDone(null)
          }}
        />
        {request.body && <p className="mt-1 text-sm text-muted">{request.body}</p>}
        <div className="mt-4 space-y-2">
          <Field label={request.label}>
            {request.type === 'reason' ? (
              <Textarea
                autoFocus
                rows={2}
                placeholder={request.placeholder}
                value={value}
                onChange={(event) => {
                  setValue(event.target.value)
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && !event.shiftKey) {
                    event.preventDefault()
                    event.currentTarget.form?.requestSubmit()
                  }
                }}
              />
            ) : (
              <Input
                autoFocus
                type={request.type === 'password' ? 'password' : 'text'}
                autoComplete={request.type === 'password' ? 'current-password' : 'off'}
                placeholder={request.placeholder}
                value={value}
                onChange={(event) => {
                  setValue(event.target.value)
                }}
              />
            )}
          </Field>
          {request.suggestions && (
            <div className="flex flex-wrap gap-1.5">
              {request.suggestions.map((suggestion) => (
                <button
                  key={suggestion}
                  type="button"
                  className="rounded-full border border-border px-2.5 py-0.5 text-2xs text-muted hover:border-accent-line hover:text-text"
                  onClick={() => {
                    setValue(suggestion)
                  }}
                >
                  {suggestion}
                </button>
              ))}
            </div>
          )}
        </div>
        <ModalFooter>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              onDone(null)
            }}
          >
            Cancel
          </Button>
          <Button type="submit" variant="primary" size="sm" disabled={!valid || busy}>
            {busy && <Loader2 className="size-3.5 animate-spin" />}
            {request.confirmLabel ?? 'Continue'}
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  )
}
