import { CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { useSyncExternalStore } from 'react'

import { cn } from '@/lib/utils'

/*
 * App-wide toasts. Call toast() from anywhere; <ToastHost/> (mounted in
 * main.tsx) renders them in a polite live region.
 */

type Tone = 'success' | 'error' | 'info'
type Toast = { id: number; message: string; tone: Tone; undo?: () => void }

let toasts: Toast[] = []
let nextId = 1
const listeners = new Set<() => void>()

function emit() {
  for (const listener of listeners) listener()
}

export function dismissToast(id: number) {
  toasts = toasts.filter((item) => item.id !== id)
  emit()
}

export function toast(message: string, options: { tone?: Tone; undo?: () => void } = {}) {
  const id = nextId++
  const item: Toast = { id, message, tone: options.tone ?? 'success' }
  if (options.undo) item.undo = options.undo
  toasts = [...toasts.slice(-3), item]
  emit()
  window.setTimeout(
    () => {
      dismissToast(id)
    },
    options.undo ? 8000 : options.tone === 'error' ? 7000 : 4000,
  )
  return id
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

const icons = { success: CheckCircle2, error: XCircle, info: Info }

export function ToastHost() {
  const items = useSyncExternalStore(
    subscribe,
    () => toasts,
    () => toasts,
  )
  return (
    <div
      aria-live="polite"
      className="pointer-events-none fixed bottom-12 right-4 z-[60] flex w-[min(380px,calc(100%-2rem))] flex-col gap-2"
    >
      {items.map((item) => {
        const Icon = icons[item.tone]
        return (
          <div
            key={item.id}
            role={item.tone === 'error' ? 'alert' : 'status'}
            className="toast-in pointer-events-auto flex items-center gap-2.5 rounded-app-sm border border-border bg-elev px-3 py-2.5 text-sm text-text shadow-app"
          >
            <Icon
              className={cn(
                'size-4 shrink-0',
                item.tone === 'success' && 'text-success',
                item.tone === 'error' && 'text-danger',
                item.tone === 'info' && 'text-blue-fg',
              )}
            />
            <span className="min-w-0 flex-1">{item.message}</span>
            {item.undo && (
              <button
                type="button"
                className="font-medium text-accent-text hover:underline"
                onClick={() => {
                  item.undo?.()
                  dismissToast(item.id)
                }}
              >
                Undo
              </button>
            )}
            <button
              type="button"
              aria-label="Dismiss"
              className="text-dim hover:text-text"
              onClick={() => {
                dismissToast(item.id)
              }}
            >
              <X className="size-3.5" />
            </button>
          </div>
        )
      })}
    </div>
  )
}
