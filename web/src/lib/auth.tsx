import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { apiErrorMessage } from '@/lib/api-error'
import { applyServerPreferences } from '@/components/theme-provider'

export type User = components['schemas']['UserBody']
export type Org = components['schemas']['OrgBody']

export type SetupForm = {
  token: string
  email: string
  name: string
  password: string
}

const ONBOARDING_KEY = 'clavis:onboarding'

type AuthStatus = 'loading' | 'setup' | 'anonymous' | 'authenticated'

type AuthContextValue = {
  status: AuthStatus
  user: User | null
  orgs: Org[]
  error: string | null
  login: (email: string, password: string) => Promise<LoginResult>
  verifyMFA: (challenge: string, code: string, recoveryCode?: string) => Promise<void>
  setup: (form: SetupForm) => Promise<void>
  logout: () => Promise<void>
  /** True right after first-run setup until the welcome flow is finished. */
  onboarding: boolean
  finishOnboarding: () => void
  /** Replaces the cached org list, for example after a rename. */
  setOrgs: (orgs: Org[]) => void
}

export type LoginResult = { mfaRequired: true; challenge: string } | { mfaRequired: false }

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('loading')
  const [user, setUser] = useState<User | null>(null)
  const [orgs, setOrgs] = useState<Org[]>([])
  const [error, setError] = useState<string | null>(null)
  const [onboarding, setOnboarding] = useState(
    () => window.localStorage.getItem(ONBOARDING_KEY) === '1',
  )

  const load = useCallback(async () => {
    try {
      const setup = await api.GET('/api/v1/setup/status')
      if (setup.data?.required) {
        setStatus('setup')
        return
      }
      const me = await api.GET('/api/v1/auth/me')
      if (me.data) {
        setUser(me.data.user)
        setOrgs(me.data.orgs ?? [])
        if (me.data.preferences) applyServerPreferences(me.data.preferences)
        setStatus('authenticated')
      } else {
        setStatus('anonymous')
      }
    } catch {
      setError('Could not reach the Clavis server.')
      setStatus('anonymous')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const login = useCallback(async (email: string, password: string) => {
    setError(null)
    const result = await api.POST('/api/v1/auth/login', { body: { email, password } })
    if (result.error || !result.data) {
      throw new Error(apiErrorMessage(result.error, 'Could not sign in.'))
    }
    if (result.data.mfa_required) {
      return { mfaRequired: true, challenge: result.data.challenge ?? '' } as const
    }
    if (!result.data.user) {
      throw new Error('Could not sign in.')
    }
    setUser(result.data.user)
    setOrgs(result.data.orgs ?? [])
    if (result.data.preferences) applyServerPreferences(result.data.preferences)
    setStatus('authenticated')
    return { mfaRequired: false } as const
  }, [])

  const verifyMFA = useCallback(async (challenge: string, code: string, recoveryCode?: string) => {
    setError(null)
    const result = await api.POST('/api/v1/auth/mfa/verify', {
      body: recoveryCode ? { challenge, code, recovery_code: recoveryCode } : { challenge, code },
    })
    if (result.error || !result.data) {
      throw new Error(apiErrorMessage(result.error, 'Could not verify the code.'))
    }
    setUser(result.data.user)
    setOrgs(result.data.orgs ?? [])
    if (result.data.preferences) applyServerPreferences(result.data.preferences)
    setStatus('authenticated')
  }, [])

  const setup = useCallback(async (form: SetupForm) => {
    setError(null)
    const result = await api.POST('/api/v1/setup', {
      body: {
        token: form.token,
        email: form.email,
        name: form.name,
        password: form.password,
        skip_project: true,
      },
    })
    if (result.error || !result.data) {
      throw new Error(apiErrorMessage(result.error, 'Setup failed.'))
    }
    setUser(result.data.user)
    setOrgs(result.data.orgs ?? [])
    if (result.data.preferences) applyServerPreferences(result.data.preferences)
    window.localStorage.setItem(ONBOARDING_KEY, '1')
    setOnboarding(true)
    setStatus('authenticated')
  }, [])

  const finishOnboarding = useCallback(() => {
    window.localStorage.removeItem(ONBOARDING_KEY)
    setOnboarding(false)
  }, [])

  const logout = useCallback(async () => {
    await api.POST('/api/v1/auth/logout')
    setUser(null)
    setOrgs([])
    setStatus('anonymous')
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      status,
      user,
      orgs,
      error,
      login,
      verifyMFA,
      setup,
      logout,
      onboarding,
      finishOnboarding,
      setOrgs,
    }),
    [status, user, orgs, error, login, verifyMFA, setup, logout, onboarding, finishOnboarding],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used inside AuthProvider')
  return value
}
