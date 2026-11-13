import { ShieldCheck } from 'lucide-react'

import { useSelfSignedFingerprint } from '@/lib/server-certificate'

/*
 * Shown next to `clavis login` on a server with a self-signed certificate:
 * the CLI asks once whether to trust it, and this is the fingerprint to
 * compare with.
 */
export function CertificateNote({ className }: { className?: string }) {
  const fingerprint = useSelfSignedFingerprint()
  if (!fingerprint) return null
  return (
    <div
      className={`rounded-app-sm border border-border bg-panel px-3 py-2 text-xs text-muted ${className ?? ''}`}
    >
      <p className="flex items-center gap-1.5 font-medium text-text">
        <ShieldCheck className="size-3.5 text-accent-text" />
        This server uses a self-signed certificate
      </p>
      <p className="mt-1">
        The first time, <span className="font-mono">clavis login</span> shows the certificate and
        asks whether to trust it. Answer yes if its SHA-256 matches:
      </p>
      <p className="mt-1.5 break-all font-mono text-[11px] text-text">{fingerprint}</p>
      <p className="mt-1.5">
        You're asked once per computer. In scripts, use{' '}
        <span className="font-mono">clavis trust add --fingerprint</span> instead.
      </p>
    </div>
  )
}
