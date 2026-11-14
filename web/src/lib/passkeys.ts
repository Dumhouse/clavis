/*
 * Passkey helpers. The server sends the WebAuthn options as the browser API
 * shapes them ({ publicKey: {...} }); @simplewebauthn/browser wants the inner
 * object.
 */

/** The options object @simplewebauthn/browser expects, from the server's. */
// eslint-disable-next-line @typescript-eslint/no-unnecessary-type-parameters -- the caller names the library's options type
export function passkeyOptions<T>(raw: unknown): T {
  if (raw && typeof raw === 'object' && 'publicKey' in raw) {
    return (raw as { publicKey: T }).publicKey
  }
  return raw as T
}

const ipv4 = /^\d{1,3}(\.\d{1,3}){3}$/

const trustSteps =
  'To trust it, copy the certificate out of the server (docker cp clavis:/data/tls/self-signed.crt ./clavis.crt), add it to this computer\'s trusted certificates (on macOS: System keychain, "Always Trust"), then reload. See "HTTPS and certificates" in the docs.'

/**
 * Why passkeys can't work on this page, or null when they can. Browsers only
 * allow them on a domain name (not an IP address) over trusted HTTPS, or on
 * localhost. selfSigned is true when the server uses its own self-signed
 * certificate.
 */
export function passkeyUnavailableReason(selfSigned = false): string | null {
  if (typeof window === 'undefined' || !('PublicKeyCredential' in window)) {
    return 'This browser does not support passkeys.'
  }
  const host = window.location.hostname
  if (ipv4.test(host) || host.includes(':') || host.startsWith('[')) {
    const certificate = selfSigned
      ? ' This server also uses a self-signed certificate, which browsers refuse for passkeys unless this computer trusts it.'
      : ''
    return `Passkeys can't be added here: browsers don't allow them on an IP address like ${host}.${certificate} Open Clavis by a name (for example clavis.home.arpa, set in your DNS or hosts file, with CLAVIS_SERVER_BASE_URL pointing at it). Passwords work in every case.`
  }
  if (!window.isSecureContext) {
    return 'Passkeys need HTTPS.'
  }
  return null
}

/**
 * A warning for a host name served with a self-signed certificate: passkeys
 * work only if this computer trusts the certificate, and pages can't tell
 * whether it does, so this doesn't block the button.
 */
export function passkeyCertificateWarning(selfSigned: boolean): string | null {
  if (!selfSigned || passkeyUnavailableReason(selfSigned) !== null) return null
  return `This server uses a self-signed certificate. Passkeys work only if this computer trusts it: if the browser warned you when you opened Clavis, adding a passkey will fail. ${trustSteps}`
}

/** A readable message for a failed passkey ceremony. */
export function passkeyErrorMessage(cause: unknown, fallback: string, selfSigned = false): string {
  if (cause instanceof Error) {
    if (cause.name === 'SecurityError') {
      return selfSigned
        ? `The browser refused passkeys because it doesn't trust this server's self-signed certificate. ${trustSteps}`
        : 'The browser refused passkeys for this address. They need a domain name with a certificate the browser trusts.'
    }
    if (cause.name === 'NotAllowedError') {
      return selfSigned
        ? "The passkey prompt was cancelled, timed out, or refused because the browser doesn't trust this server's self-signed certificate. If no prompt appeared, trust the certificate first."
        : 'The passkey prompt was cancelled or timed out.'
    }
    return cause.message
  }
  return fallback
}
