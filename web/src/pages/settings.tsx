import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import {
  GitBranch,
  KeyRound,
  Layers,
  Loader2,
  LogOut,
  Monitor,
  Palette,
  Plus,
  ShieldCheck,
  Smartphone,
  X,
} from 'lucide-react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { MfaSettings } from '@/components/mfa-settings'
import { PersonalTokens } from '@/components/personal-tokens'
import { Passkeys } from '@/components/passkeys'
import { ScimSettings } from '@/components/scim-settings'
import { WebhookSettings } from '@/components/webhook-settings'
import { Button } from '@/components/ui/button'
import { confirmAction } from '@/components/ui/dialogs'
import { Input, Notice, Select } from '@/components/ui/field'
import { useTheme } from '@/components/theme-provider'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth } from '@/lib/auth'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'

type SessionInfo = components['schemas']['SessionInfo']
type EnvTemplate = components['schemas']['EnvironmentTemplate']

export function SettingsPage({ scope = 'org' }: { scope?: 'account' | 'org' }) {
  const { user, logout } = useAuth()
  const { org } = useCurrentOrg()
  const { theme, mode, appearance, setTheme, setAppearance } = useTheme()
  const queryClient = useQueryClient()

  const [verifyMessage, setVerifyMessage] = useState<string | null>(null)
  const [hintsOn, setHintsOn] = useState(() => localStorage.getItem('clavis.hints') !== 'false')
  const [orgAppearance, setOrgAppearance] = useState(org?.default_appearance ?? 'classic')
  const [orgMessage, setOrgMessage] = useState<string | null>(null)
  const [requireMFA, setRequireMFA] = useState(false)
  const [idleHours, setIdleHours] = useState(0)
  const [absoluteDays, setAbsoluteDays] = useState(0)
  const [allowlist, setAllowlist] = useState('')
  const [blockCLIWrites, setBlockCLIWrites] = useState(false)
  const [duplicateDetection, setDuplicateDetection] = useState(false)
  const [securityMessage, setSecurityMessage] = useState<string | null>(null)
  const [securityBusy, setSecurityBusy] = useState(false)
  const [templates, setTemplates] = useState<EnvTemplate[]>([])
  const [ssoEnabled, setSsoEnabled] = useState(false)
  const [ssoProvider, setSsoProvider] = useState('generic')
  const [ssoClientID, setSsoClientID] = useState('')
  const [ssoClientSecret, setSsoClientSecret] = useState('')
  const [ssoSecretSet, setSsoSecretSet] = useState(false)
  const [ssoIssuer, setSsoIssuer] = useState('')
  const [ssoIdPMetadata, setSsoIdPMetadata] = useState('')
  const [ssoEnforce, setSsoEnforce] = useState(false)
  const [ssoDomains, setSsoDomains] = useState('')
  const [ssoRole, setSsoRole] = useState('Developer')
  const [ssoBusy, setSsoBusy] = useState(false)
  const [ssoMessage, setSsoMessage] = useState<string | null>(null)
  const [previewEnabled, setPreviewEnabled] = useState(false)
  const [previewSecret, setPreviewSecret] = useState('')
  const [previewSecretSet, setPreviewSecretSet] = useState(false)
  const [previewDays, setPreviewDays] = useState(7)
  const [previewBusy, setPreviewBusy] = useState(false)
  const [previewMessage, setPreviewMessage] = useState<string | null>(null)

  const orgSettings = useQuery({
    queryKey: ['org-settings', org?.slug],
    enabled: Boolean(org && (org.role === 'Owner' || org.role === 'Admin')),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/settings', {
        params: { path: { org: org.slug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load organization settings.'))
      return result.data
    },
  })

  useEffect(() => {
    if (!orgSettings.data) return
    setRequireMFA(orgSettings.data.require_mfa ?? false)
    setIdleHours(orgSettings.data.session_idle_hours ?? 0)
    setAbsoluteDays(orgSettings.data.session_absolute_days ?? 0)
    setAllowlist((orgSettings.data.ip_allowlist ?? []).join('\n'))
    setBlockCLIWrites(orgSettings.data.block_cli_writes ?? false)
    setDuplicateDetection(orgSettings.data.duplicate_detection ?? false)
    setTemplates(orgSettings.data.environment_templates ?? [])
    setSsoEnabled(orgSettings.data.sso.enabled)
    setSsoProvider(orgSettings.data.sso.provider || 'generic')
    setSsoClientID(orgSettings.data.sso.client_id)
    setSsoSecretSet(orgSettings.data.sso.client_secret_set)
    setSsoIssuer(orgSettings.data.sso.issuer)
    setSsoIdPMetadata(orgSettings.data.sso.idp_metadata ?? '')
    setSsoEnforce(orgSettings.data.sso.enforce)
    setSsoDomains((orgSettings.data.sso.allowed_domains ?? []).join(', '))
    setSsoRole(orgSettings.data.sso.role || 'Developer')
    setPreviewEnabled(orgSettings.data.preview_enabled)
    setPreviewSecretSet(orgSettings.data.github_webhook_secret_set)
    setPreviewDays(orgSettings.data.preview_retention_days || 7)
    setOrgAppearance(orgSettings.data.default_appearance ?? 'classic')
  }, [orgSettings.data])

  async function saveSecurityPolicy() {
    if (!org) return
    setSecurityBusy(true)
    setSecurityMessage(null)
    const result = await api.PATCH('/api/v1/orgs/{org}/settings', {
      params: { path: { org: org.slug } },
      body: {
        require_mfa: requireMFA,
        session_idle_hours: idleHours,
        session_absolute_days: absoluteDays,
        block_cli_writes: blockCLIWrites,
        duplicate_detection: duplicateDetection,
        environment_templates: templates,
        ip_allowlist: allowlist
          .split(/\n|,/)
          .map((entry) => entry.trim())
          .filter((entry) => entry.length > 0),
      },
    })
    setSecurityBusy(false)
    setSecurityMessage(
      result.error
        ? apiErrorMessage(result.error, 'Could not save security policy.')
        : 'Security policy saved.',
    )
  }

  async function saveSSO() {
    if (!org) return
    setSsoBusy(true)
    setSsoMessage(null)
    const sso: {
      enabled: boolean
      provider: 'generic' | 'google' | 'github' | 'microsoft' | 'okta' | 'saml'
      client_id: string
      issuer: string
      enforce: boolean
      allowed_domains: string[]
      role: string
      client_secret?: string
      idp_metadata?: string
    } = {
      enabled: ssoEnabled,
      provider: ssoProvider as 'generic' | 'google' | 'github' | 'microsoft' | 'okta' | 'saml',
      client_id: ssoClientID,
      issuer: ssoIssuer,
      enforce: ssoEnforce,
      allowed_domains: ssoDomains
        .split(/[,\n]/)
        .map((entry) => entry.trim())
        .filter((entry) => entry.length > 0),
      role: ssoRole,
    }
    if (ssoProvider === 'saml') sso.idp_metadata = ssoIdPMetadata
    else if (ssoClientSecret !== '') sso.client_secret = ssoClientSecret
    const result = await api.PATCH('/api/v1/orgs/{org}/settings', {
      params: { path: { org: org.slug } },
      body: { sso },
    })
    setSsoBusy(false)
    if (result.error) {
      setSsoMessage(apiErrorMessage(result.error, 'Could not save SSO settings.'))
      return
    }
    setSsoClientSecret('')
    setSsoSecretSet(true)
    setSsoMessage('SSO settings saved.')
    await queryClient.invalidateQueries({ queryKey: ['org-settings', org.slug] })
  }

  async function savePreview() {
    if (!org) return
    setPreviewBusy(true)
    setPreviewMessage(null)
    const body: {
      preview_enabled: boolean
      preview_retention_days: number
      github_webhook_secret?: string
    } = { preview_enabled: previewEnabled, preview_retention_days: previewDays }
    if (previewSecret !== '') body.github_webhook_secret = previewSecret
    const result = await api.PATCH('/api/v1/orgs/{org}/settings', {
      params: { path: { org: org.slug } },
      body,
    })
    setPreviewBusy(false)
    if (result.error) {
      setPreviewMessage(apiErrorMessage(result.error, 'Could not save preview settings.'))
      return
    }
    setPreviewSecret('')
    setPreviewSecretSet(true)
    setPreviewMessage('Preview environment settings saved.')
    await queryClient.invalidateQueries({ queryKey: ['org-settings', org.slug] })
  }

  async function saveOrgAppearance(value: string) {
    if (!org) return
    setOrgAppearance(value)
    const result = await api.PATCH('/api/v1/orgs/{org}/settings', {
      params: { path: { org: org.slug } },
      body: { default_appearance: value as 'classic' | 'warm' },
    })
    setOrgMessage(
      result.error
        ? apiErrorMessage(result.error, 'Could not save the default.')
        : 'Organization default saved.',
    )
  }

  async function resendVerification() {
    const result = await api.POST('/api/v1/auth/verify/request')
    setVerifyMessage(
      result.error
        ? apiErrorMessage(result.error, 'Could not send the verification email.')
        : 'Verification email requested.',
    )
  }

  async function saveAppearance(
    nextTheme: 'system' | 'dark' | 'light',
    nextAppearance: 'classic' | 'warm',
  ) {
    setTheme(nextTheme)
    setAppearance(nextAppearance)
    await api.PATCH('/api/v1/auth/preferences', {
      body: { theme: nextTheme, appearance: nextAppearance },
    })
  }

  const sessions = useQuery({
    queryKey: ['auth', 'sessions'],
    queryFn: async () => {
      const result = await api.GET('/api/v1/auth/sessions')
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load sessions.'))
      return result.data?.sessions ?? []
    },
  })

  const revoke = useMutation({
    mutationFn: async (session: SessionInfo) => {
      const result = await api.DELETE('/api/v1/auth/sessions/{id}', {
        params: { path: { id: session.id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not revoke the session.'))
      return session
    },
    onSuccess: async (session) => {
      await queryClient.invalidateQueries({ queryKey: ['auth', 'sessions'] })
      if (session.current) await logout()
    },
  })

  const revokeAll = useMutation({
    mutationFn: async () => {
      const result = await api.POST('/api/v1/auth/sessions/revoke-all')
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not sign out everywhere.'))
    },
    onSuccess: async () => {
      await logout()
    },
  })

  const canAdmin = org?.role === 'Owner' || org?.role === 'Admin'

  return (
    <div className="space-y-6">
      <header>
        <h1 className="font-head text-2xl font-semibold tracking-tight">
          {scope === 'account' ? 'Your account' : 'Organization settings'}
        </h1>
        <p className="mt-1 text-base text-muted">
          {scope === 'account'
            ? `Sign-in methods, sessions, tokens, and appearance for ${user?.email ?? 'you'}. These follow you across organizations.`
            : `Security policy, single sign-on, and defaults for everyone in ${org?.name ?? 'this organization'}.`}
        </p>
        <nav aria-label="Settings" className="mt-4 flex gap-1 border-b border-border">
          {(
            [
              ['/account', 'Your account', 'account'],
              ['/settings', 'Organization', 'org'],
            ] as const
          ).map(([to, label, value]) => (
            <Link
              key={to}
              to={to}
              aria-current={scope === value ? 'page' : undefined}
              className={cn(
                '-mb-px border-b-2 px-3 pb-2 text-sm',
                scope === value
                  ? 'border-accent font-medium text-text'
                  : 'border-transparent text-muted hover:text-text',
              )}
            >
              {label}
            </Link>
          ))}
        </nav>
      </header>

      <div className="grid gap-8 lg:grid-cols-[13rem_minmax(0,1fr)]">
        <SettingsNav items={scope === 'account' ? accountNav : orgNav(canAdmin)} />
        <div className="min-w-0 space-y-6">
          {scope === 'account' ? (
            <>
              <section
                id="account"
                className="scroll-mt-4 rounded-app border border-border bg-surface"
              >
                <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                  <ShieldCheck className="size-4 text-muted" />
                  <h2 className="text-base font-semibold">Account</h2>
                </div>
                <div className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium">{user?.email}</p>
                    <p className="text-xs text-dim">
                      {user?.email_verified ? 'Email verified' : 'Email not verified yet'}
                    </p>
                    {verifyMessage && <p className="mt-1 text-xs text-muted">{verifyMessage}</p>}
                  </div>
                  {user?.email_verified ? (
                    <span className="rounded-full bg-success-bg px-2 py-0.5 text-2xs font-semibold text-success">
                      Verified
                    </span>
                  ) : (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        void resendVerification()
                      }}
                    >
                      Resend verification
                    </Button>
                  )}
                </div>
              </section>

              <section
                id="appearance"
                className="scroll-mt-4 rounded-app border border-border bg-surface"
              >
                <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                  <Palette className="size-4 text-muted" />
                  <h2 className="text-base font-semibold">Appearance</h2>
                </div>
                <div className="space-y-4 p-4">
                  <div>
                    <p className="text-sm font-medium">Theme</p>
                    <p className="text-xs text-muted">
                      System, dark, or light. Saved in this browser.
                    </p>
                    <div className="mt-2 flex gap-2">
                      <Button
                        variant={mode === 'system' ? 'primary' : 'secondary'}
                        size="sm"
                        onClick={() => {
                          void saveAppearance('system', appearance)
                        }}
                      >
                        System
                      </Button>
                      <Button
                        variant={mode === 'dark' ? 'primary' : 'secondary'}
                        size="sm"
                        onClick={() => {
                          void saveAppearance('dark', appearance)
                        }}
                      >
                        Dark
                      </Button>
                      <Button
                        variant={mode === 'light' ? 'primary' : 'secondary'}
                        size="sm"
                        onClick={() => {
                          void saveAppearance('light', appearance)
                        }}
                      >
                        Light
                      </Button>
                    </div>
                  </div>
                  <div>
                    <p className="text-sm font-medium">Style</p>
                    <p className="text-xs text-muted">
                      Classic is dense and neutral; Warm uses stone surfaces and larger radii.
                    </p>
                    <div className="mt-2 flex gap-2">
                      <Button
                        variant={appearance === 'classic' ? 'primary' : 'secondary'}
                        size="sm"
                        onClick={() => {
                          void saveAppearance(theme, 'classic')
                        }}
                      >
                        Classic
                      </Button>
                      <Button
                        variant={appearance === 'warm' ? 'primary' : 'secondary'}
                        size="sm"
                        onClick={() => {
                          void saveAppearance(theme, 'warm')
                        }}
                      >
                        Warm
                      </Button>
                    </div>
                  </div>
                  <label className="flex items-center gap-2 text-sm text-muted">
                    <input
                      type="checkbox"
                      checked={hintsOn}
                      onChange={(event) => {
                        const next = event.target.checked
                        setHintsOn(next)
                        localStorage.setItem('clavis.hints', next ? 'true' : 'false')
                        window.dispatchEvent(new CustomEvent('clavis:hints'))
                      }}
                    />
                    Show the page hint bar
                  </label>
                </div>
              </section>

              <div id="mfa" className="scroll-mt-4">
                <MfaSettings />
              </div>

              <div id="passkeys" className="scroll-mt-4">
                <Passkeys />
              </div>

              <section
                id="sessions"
                className="scroll-mt-4 rounded-app border border-border bg-surface"
              >
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
                  <div className="flex items-center gap-2">
                    <ShieldCheck className="size-4 text-muted" />
                    <h2 className="text-base font-semibold">Active sessions</h2>
                    {sessions.data && (
                      <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">
                        {sessions.data.length}
                      </span>
                    )}
                  </div>
                  <Button
                    variant="danger"
                    size="sm"
                    onClick={() => {
                      void confirmAction(
                        'Sign out everywhere? Every session ends, including this one.',
                        'Sign out everywhere',
                      ).then((ok) => {
                        if (ok) revokeAll.mutate()
                      })
                    }}
                    disabled={revokeAll.isPending}
                  >
                    {revokeAll.isPending && <Loader2 className="size-3.5 animate-spin" />} Sign out
                    everywhere
                  </Button>
                </div>

                {sessions.isLoading && (
                  <div className="flex items-center gap-2 px-4 py-5 text-sm text-muted">
                    <Loader2 className="size-4 animate-spin" /> Loading sessions…
                  </div>
                )}
                {sessions.isError && (
                  <p className="px-4 py-4 text-sm text-danger">
                    {sessions.error instanceof Error
                      ? sessions.error.message
                      : 'Could not load sessions.'}
                  </p>
                )}
                <div className="divide-y divide-border-soft">
                  {(sessions.data ?? []).map((session) => (
                    <div key={session.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                      <div className="flex size-8 items-center justify-center rounded-app-sm bg-elev text-dim">
                        {session.user_agent.toLowerCase().includes('mobile') ? (
                          <Smartphone className="size-4" />
                        ) : (
                          <Monitor className="size-4" />
                        )}
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">
                          {sessionLabel(session.user_agent)}
                          {session.current && (
                            <span className="ml-2 rounded-full bg-accent-soft px-1.5 py-0.5 text-2xs font-semibold text-accent-text">
                              this device
                            </span>
                          )}
                        </p>
                        <p className="truncate text-xs text-dim">
                          {session.ip || 'unknown IP'} · last seen{' '}
                          {formatTime(session.last_seen_at)} · expires{' '}
                          {formatTime(session.expires_at)}
                        </p>
                      </div>
                      <Button
                        variant={session.current ? 'danger' : 'secondary'}
                        size="sm"
                        onClick={() => {
                          revoke.mutate(session)
                        }}
                        disabled={revoke.isPending}
                      >
                        {session.current ? (
                          <LogOut className="size-3.5" />
                        ) : (
                          <KeyRound className="size-3.5" />
                        )}
                        {session.current ? 'Sign out' : 'Revoke'}
                      </Button>
                    </div>
                  ))}
                  {sessions.data && sessions.data.length === 0 && (
                    <p className="px-4 py-5 text-sm text-muted">No active sessions.</p>
                  )}
                </div>
                {revoke.error && (
                  <p className="px-4 pb-3 text-xs text-danger">
                    {revoke.error instanceof Error
                      ? revoke.error.message
                      : 'Could not revoke the session.'}
                  </p>
                )}
              </section>

              <div id="tokens" className="scroll-mt-4">
                <PersonalTokens />
              </div>
            </>
          ) : (
            <>
              {!canAdmin && (
                <Notice tone="info">
                  Only Owners and Admins can change organization settings. You are{' '}
                  {org?.role ?? 'a member'}.
                </Notice>
              )}
              <section
                id="org-default"
                className="scroll-mt-4 rounded-app border border-border bg-surface"
              >
                <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                  <Palette className="size-4 text-muted" />
                  <h2 className="text-base font-semibold">Organization default</h2>
                </div>
                <div className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium">Appearance for new members</p>
                    <p className="text-xs text-dim">
                      Applied when someone accepts an invite. They can change it later.
                    </p>
                    {orgMessage && <p className="mt-1 text-xs text-muted">{orgMessage}</p>}
                  </div>
                  <Select
                    className="h-8"
                    value={orgAppearance}
                    onChange={(event) => {
                      void saveOrgAppearance(event.target.value)
                    }}
                    disabled={org?.role !== 'Owner' && org?.role !== 'Admin'}
                  >
                    <option value="classic">Classic</option>
                    <option value="warm">Warm</option>
                  </Select>
                </div>
              </section>

              {(org?.role === 'Owner' || org?.role === 'Admin') && (
                <section
                  id="security"
                  className="scroll-mt-4 rounded-app border border-border bg-surface"
                >
                  <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                    <ShieldCheck className="size-4 text-muted" />
                    <h2 className="text-base font-semibold">Security policy</h2>
                  </div>
                  <div className="space-y-4 p-4">
                    <label className="flex items-start gap-2 text-sm">
                      <input
                        type="checkbox"
                        className="mt-0.5"
                        checked={requireMFA}
                        onChange={(event) => {
                          setRequireMFA(event.target.checked)
                        }}
                      />
                      <span>
                        <span className="block font-medium">Require two-factor authentication</span>
                        <span className="block text-xs text-muted">
                          Members without MFA can still sign in and enable it, but org data stays
                          blocked until they do.
                        </span>
                      </span>
                    </label>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <label className="block">
                        <span className="mb-1 block text-xs font-medium text-muted">
                          Idle timeout (hours)
                        </span>
                        <Input
                          type="number"
                          min={0}
                          max={168}
                          className="h-9"
                          value={idleHours}
                          onChange={(event) => {
                            setIdleHours(Number(event.target.value))
                          }}
                        />
                        <span className="mt-1 block text-2xs text-dim">
                          0 uses the instance default (12h).
                        </span>
                      </label>
                      <label className="block">
                        <span className="mb-1 block text-xs font-medium text-muted">
                          Absolute session lifetime (days)
                        </span>
                        <Input
                          type="number"
                          min={0}
                          max={365}
                          className="h-9"
                          value={absoluteDays}
                          onChange={(event) => {
                            setAbsoluteDays(Number(event.target.value))
                          }}
                        />
                        <span className="mt-1 block text-2xs text-dim">
                          0 uses the instance default (7 days).
                        </span>
                      </label>
                    </div>
                    <label className="block">
                      <span className="mb-1 block text-xs font-medium text-muted">
                        IP allowlist
                      </span>
                      <textarea
                        rows={3}
                        placeholder={'10.0.0.0/8\n203.0.113.42'}
                        className="w-full rounded-app-sm border border-border bg-bg px-2.5 py-2 font-mono text-sm text-text focus:border-accent-line focus:outline-none"
                        value={allowlist}
                        onChange={(event) => {
                          setAllowlist(event.target.value)
                        }}
                      />
                      <span className="mt-1 block text-2xs text-dim">
                        One CIDR or IP per line. Empty allows every address. Make sure your current
                        network is included before saving.
                      </span>
                    </label>
                    {securityMessage && <p className="text-xs text-muted">{securityMessage}</p>}
                    <div className="flex justify-end">
                      <Button
                        variant="primary"
                        size="sm"
                        onClick={() => {
                          void saveSecurityPolicy()
                        }}
                        disabled={securityBusy}
                      >
                        {securityBusy && <Loader2 className="size-3.5 animate-spin" />} Save policy
                      </Button>
                    </div>
                  </div>
                </section>
              )}

              {(org?.role === 'Owner' || org?.role === 'Admin') && (
                <section
                  id="sso"
                  className="scroll-mt-4 rounded-app border border-border bg-surface"
                >
                  <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                    <KeyRound className="size-4 text-muted" />
                    <h2 className="text-base font-semibold">Single sign-on</h2>
                  </div>
                  <div className="space-y-3 p-4">
                    <label className="flex items-start gap-2 text-sm">
                      <input
                        type="checkbox"
                        className="mt-0.5"
                        checked={ssoEnabled}
                        onChange={(event) => {
                          setSsoEnabled(event.target.checked)
                        }}
                      />
                      <span>
                        <span className="block font-medium">Enable SSO</span>
                        <span className="block text-xs text-muted">
                          OIDC for Google, Microsoft, Okta, and generic providers; OAuth for GitHub.
                        </span>
                      </span>
                    </label>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <label className="block">
                        <span className="mb-1 block text-xs font-medium text-muted">Provider</span>
                        <Select
                          className="h-9"
                          value={ssoProvider}
                          onChange={(event) => {
                            setSsoProvider(event.target.value)
                          }}
                        >
                          <option value="generic">Generic OIDC</option>
                          <option value="google">Google</option>
                          <option value="microsoft">Microsoft</option>
                          <option value="okta">Okta</option>
                          <option value="github">GitHub</option>
                          <option value="saml">SAML 2.0</option>
                        </Select>
                      </label>
                      <label className="block">
                        <span className="mb-1 block text-xs font-medium text-muted">
                          Auto-join role
                        </span>
                        <Select
                          className="h-9"
                          value={ssoRole}
                          onChange={(event) => {
                            setSsoRole(event.target.value)
                          }}
                        >
                          <option value="Viewer">Viewer</option>
                          <option value="Developer">Developer</option>
                          <option value="Admin">Admin</option>
                        </Select>
                      </label>
                      {ssoProvider !== 'saml' && (
                        <>
                          <label className="block">
                            <span className="mb-1 block text-xs font-medium text-muted">
                              Client ID
                            </span>
                            <Input
                              className="h-9 font-mono"
                              value={ssoClientID}
                              onChange={(event) => {
                                setSsoClientID(event.target.value)
                              }}
                            />
                          </label>
                          <label className="block">
                            <span className="mb-1 block text-xs font-medium text-muted">
                              Client secret {ssoSecretSet ? '(stored)' : ''}
                            </span>
                            <Input
                              type="password"
                              className="h-9 font-mono"
                              placeholder={ssoSecretSet ? 'Leave blank to keep current secret' : ''}
                              value={ssoClientSecret}
                              onChange={(event) => {
                                setSsoClientSecret(event.target.value)
                              }}
                            />
                          </label>
                          {ssoProvider !== 'google' &&
                            ssoProvider !== 'microsoft' &&
                            ssoProvider !== 'github' && (
                              <label className="block sm:col-span-2">
                                <span className="mb-1 block text-xs font-medium text-muted">
                                  Issuer URL
                                </span>
                                <Input
                                  className="h-9 font-mono"
                                  placeholder="https://idp.example.com"
                                  value={ssoIssuer}
                                  onChange={(event) => {
                                    setSsoIssuer(event.target.value)
                                  }}
                                />
                              </label>
                            )}
                        </>
                      )}
                      {ssoProvider === 'saml' && (
                        <label className="block sm:col-span-2">
                          <span className="mb-1 block text-xs font-medium text-muted">
                            IdP metadata (URL or XML)
                          </span>
                          <textarea
                            rows={4}
                            className="w-full rounded-app-sm border border-border bg-bg px-2.5 py-2 font-mono text-xs text-text focus:border-accent-line focus:outline-none"
                            placeholder="https://idp.example.com/app/.../sso/saml/metadata"
                            value={ssoIdPMetadata}
                            onChange={(event) => {
                              setSsoIdPMetadata(event.target.value)
                            }}
                          />
                          <span className="mt-1 block text-2xs text-dim">
                            SP metadata: {window.location.origin}/saml/{org?.slug ?? 'org'}/metadata
                          </span>
                        </label>
                      )}
                      <label className="block sm:col-span-2">
                        <span className="mb-1 block text-xs font-medium text-muted">
                          Allowed email domains
                        </span>
                        <Input
                          className="h-9"
                          placeholder="example.com, company.com"
                          value={ssoDomains}
                          onChange={(event) => {
                            setSsoDomains(event.target.value)
                          }}
                        />
                      </label>
                    </div>
                    <label className="flex items-start gap-2 text-sm">
                      <input
                        type="checkbox"
                        className="mt-0.5"
                        checked={ssoEnforce}
                        onChange={(event) => {
                          setSsoEnforce(event.target.checked)
                        }}
                      />
                      <span>
                        <span className="block font-medium">
                          Require SSO for members of this organization
                        </span>
                        <span className="block text-xs text-muted">
                          Password sign-in is blocked for members of an enforcing organization.
                        </span>
                      </span>
                    </label>
                    {ssoMessage && <p className="text-xs text-muted">{ssoMessage}</p>}
                    <div className="flex justify-end">
                      <Button
                        variant="primary"
                        size="sm"
                        disabled={ssoBusy}
                        onClick={() => {
                          void saveSSO()
                        }}
                      >
                        {ssoBusy && <Loader2 className="size-3.5 animate-spin" />} Save SSO
                      </Button>
                    </div>
                  </div>
                </section>
              )}

              {(org?.role === 'Owner' || org?.role === 'Admin') && (
                <section
                  id="previews"
                  className="scroll-mt-4 rounded-app border border-border bg-surface"
                >
                  <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                    <GitBranch className="size-4 text-muted" />
                    <h2 className="text-base font-semibold">Preview environments</h2>
                  </div>
                  <div className="space-y-3 p-4">
                    <label className="flex items-start gap-2 text-sm">
                      <input
                        type="checkbox"
                        className="mt-0.5"
                        checked={previewEnabled}
                        onChange={(event) => {
                          setPreviewEnabled(event.target.checked)
                        }}
                      />
                      <span>
                        <span className="block font-medium">
                          Create preview environments from pull requests
                        </span>
                        <span className="block text-xs text-muted">
                          Point the repository webhook at{' '}
                          <code className="font-mono">
                            {window.location.origin}/api/v1/orgs/{org?.slug}
                            /projects/&lt;project&gt;/integrations/github/webhook
                          </code>
                          . PRs inherit the development environment and expire automatically.
                        </span>
                      </span>
                    </label>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <label className="block">
                        <span className="mb-1 block text-xs font-medium text-muted">
                          Webhook secret {previewSecretSet ? '(stored)' : ''}
                        </span>
                        <Input
                          type="password"
                          className="h-9 font-mono"
                          placeholder={previewSecretSet ? 'Leave blank to keep current secret' : ''}
                          value={previewSecret}
                          onChange={(event) => {
                            setPreviewSecret(event.target.value)
                          }}
                        />
                      </label>
                      <label className="block">
                        <span className="mb-1 block text-xs font-medium text-muted">
                          Retention (days)
                        </span>
                        <Input
                          type="number"
                          min={1}
                          max={90}
                          className="h-9"
                          value={previewDays}
                          onChange={(event) => {
                            setPreviewDays(Number(event.target.value))
                          }}
                        />
                      </label>
                    </div>
                    {previewMessage && <p className="text-xs text-muted">{previewMessage}</p>}
                    <div className="flex justify-end">
                      <Button
                        variant="primary"
                        size="sm"
                        disabled={previewBusy}
                        onClick={() => {
                          void savePreview()
                        }}
                      >
                        {previewBusy && <Loader2 className="size-3.5 animate-spin" />} Save previews
                      </Button>
                    </div>
                  </div>
                </section>
              )}

              {canAdmin && (
                <div id="scim" className="scroll-mt-4">
                  <ScimSettings />
                </div>
              )}

              {canAdmin && (
                <div id="webhooks" className="scroll-mt-4">
                  <WebhookSettings />
                </div>
              )}

              {(org?.role === 'Owner' || org?.role === 'Admin') && (
                <section
                  id="templates"
                  className="scroll-mt-4 rounded-app border border-border bg-surface"
                >
                  <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                    <Layers className="size-4 text-muted" />
                    <h2 className="text-base font-semibold">Environment templates</h2>
                    <span className="text-xs text-dim">Applied to new projects</span>
                  </div>
                  <div className="space-y-3 p-4">
                    {templates.length === 0 && (
                      <p className="text-xs text-muted">
                        New projects get dev, staging, and prod. Add templates to customise that
                        set.
                      </p>
                    )}
                    {templates.map((template, index) => (
                      <div
                        key={`${template.slug}-${String(index)}`}
                        className="flex flex-wrap items-end gap-2"
                      >
                        <label className="block">
                          <span className="mb-1 block text-2xs font-medium text-muted">Slug</span>
                          <Input
                            className="h-8 w-28 font-mono"
                            value={template.slug}
                            onChange={(event) => {
                              setTemplates((current) =>
                                current.map((item, itemIndex) =>
                                  itemIndex === index
                                    ? { ...item, slug: event.target.value }
                                    : item,
                                ),
                              )
                            }}
                          />
                        </label>
                        <label className="block">
                          <span className="mb-1 block text-2xs font-medium text-muted">Name</span>
                          <Input
                            className="h-8 w-40"
                            value={template.name}
                            onChange={(event) => {
                              setTemplates((current) =>
                                current.map((item, itemIndex) =>
                                  itemIndex === index
                                    ? { ...item, name: event.target.value }
                                    : item,
                                ),
                              )
                            }}
                          />
                        </label>
                        <label className="block">
                          <span className="mb-1 block text-2xs font-medium text-muted">Type</span>
                          <Select
                            className="h-8"
                            value={template.type}
                            onChange={(event) => {
                              const value = event.target.value as EnvTemplate['type']
                              setTemplates((current) =>
                                current.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, type: value } : item,
                                ),
                              )
                            }}
                          >
                            <option value="development">development</option>
                            <option value="preview">preview</option>
                            <option value="staging">staging</option>
                            <option value="production">production</option>
                          </Select>
                        </label>
                        <label className="flex items-center gap-1.5 pb-1.5 text-xs">
                          <input
                            type="checkbox"
                            checked={template.protected ?? false}
                            onChange={(event) => {
                              setTemplates((current) =>
                                current.map((item, itemIndex) =>
                                  itemIndex === index
                                    ? { ...item, protected: event.target.checked }
                                    : item,
                                ),
                              )
                            }}
                          />
                          Protected
                        </label>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label="Remove template"
                          onClick={() => {
                            setTemplates((current) =>
                              current.filter((_, itemIndex) => itemIndex !== index),
                            )
                          }}
                        >
                          <X className="size-3.5" />
                        </Button>
                      </div>
                    ))}
                    <div className="flex items-center justify-between gap-2">
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => {
                          setTemplates((current) => [
                            ...current,
                            { slug: '', name: '', type: 'development', protected: false },
                          ])
                        }}
                      >
                        <Plus className="size-3.5" /> Add template
                      </Button>
                      <Button
                        variant="primary"
                        size="sm"
                        onClick={() => {
                          void saveSecurityPolicy()
                        }}
                        disabled={securityBusy}
                      >
                        {securityBusy && <Loader2 className="size-3.5 animate-spin" />} Save
                        templates
                      </Button>
                    </div>
                    {securityMessage && <p className="text-xs text-muted">{securityMessage}</p>}
                  </div>
                </section>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

const accountNav: NavItem[] = [
  { id: 'account', label: 'Profile' },
  { id: 'appearance', label: 'Appearance' },
  { id: 'mfa', label: 'Two-factor auth' },
  { id: 'passkeys', label: 'Passkeys' },
  { id: 'sessions', label: 'Active sessions' },
  { id: 'tokens', label: 'Access tokens' },
]

function orgNav(canAdmin: boolean): NavItem[] {
  return [
    { id: 'org-default', label: 'Defaults' },
    ...(canAdmin
      ? [
          { id: 'security', label: 'Security policy' },
          { id: 'sso', label: 'Single sign-on' },
          { id: 'previews', label: 'Preview environments' },
          { id: 'scim', label: 'SCIM provisioning' },
          { id: 'webhooks', label: 'Webhooks' },
          { id: 'templates', label: 'Environment templates' },
        ]
      : []),
  ]
}

type NavItem = { id: string; label: string }

/** Sticky section index with scroll-spy, like the settings pages of GitHub and Vercel. */
function SettingsNav({ items }: { items: NavItem[] }) {
  const [active, setActive] = useState(items[0]?.id ?? '')

  useEffect(() => {
    const root = document.querySelector<HTMLElement>('[data-scroll-region]')
    const visible = new Set<string>()
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) visible.add(entry.target.id)
          else visible.delete(entry.target.id)
        }
        const first = items.find((item) => visible.has(item.id))
        if (first) setActive(first.id)
      },
      { root, rootMargin: '0px 0px -60% 0px' },
    )
    for (const item of items) {
      const element = document.getElementById(item.id)
      if (element) observer.observe(element)
    }
    return () => {
      observer.disconnect()
    }
  }, [items])

  return (
    <nav aria-label="Sections" className="hidden lg:block">
      <ul className="sticky top-2 space-y-px">
        {items.map((item) => (
          <li key={item.id}>
            <a
              href={`#${item.id}`}
              aria-current={active === item.id ? 'location' : undefined}
              onClick={(event) => {
                event.preventDefault()
                setActive(item.id)
                document.getElementById(item.id)?.scrollIntoView({ behavior: 'smooth' })
              }}
              className={cn(
                'block rounded-app-sm px-2.5 py-1.5 text-sm transition-colors',
                active === item.id
                  ? 'bg-hover font-medium text-text'
                  : 'text-muted hover:bg-hover hover:text-text',
              )}
            >
              {item.label}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  )
}

function sessionLabel(userAgent: string): string {
  if (!userAgent) return 'Unknown device'
  if (userAgent.includes('Edg/')) return 'Microsoft Edge'
  if (userAgent.includes('Chrome/')) return 'Chrome'
  if (userAgent.includes('Safari/')) return 'Safari'
  if (userAgent.includes('Firefox/')) return 'Firefox'
  return userAgent.slice(0, 60)
}

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export function OrgSettingsPage() {
  return <SettingsPage scope="org" />
}

export function AccountSettingsPage() {
  return <SettingsPage scope="account" />
}
