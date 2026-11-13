import { Check, Copy } from 'lucide-react'
import { useState } from 'react'

import { cn } from '@/lib/utils'

/**
 * Copies text to the clipboard. navigator.clipboard only exists in secure
 * contexts (https or localhost), so a plain-HTTP home-lab install falls back to
 * a hidden textarea and execCommand, which still works there.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through to the legacy path
  }
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.appendChild(area)
  area.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  area.remove()
  return ok
}

/** A one-line command with a copy button. */
export function CopyCommand({ command, className }: { command: string; className?: string }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    if (await copyText(command)) {
      setCopied(true)
      window.setTimeout(() => {
        setCopied(false)
      }, 1500)
    }
  }

  return (
    <div
      className={cn(
        'flex min-w-0 items-center gap-2 rounded-app-sm border border-border bg-bg py-1 pl-3 pr-1',
        className,
      )}
    >
      <code className="min-w-0 flex-1 break-all py-1 font-mono text-xs text-accent-text">
        {command}
      </code>
      <button
        type="button"
        aria-label={copied ? 'Copied' : `Copy ${command}`}
        title={copied ? 'Copied' : 'Copy'}
        className="flex size-7 shrink-0 items-center justify-center rounded-app-sm text-muted hover:bg-hover hover:text-text"
        onClick={() => {
          void copy()
        }}
      >
        {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
      </button>
    </div>
  )
}
