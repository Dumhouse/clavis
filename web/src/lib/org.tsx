import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

import type { Org } from '@/lib/auth'
import { useAuth } from '@/lib/auth'

type OrgContextValue = {
  org: Org | undefined
  orgs: Org[]
  selectOrg: (slug: string) => void
}

const OrgContext = createContext<OrgContextValue | null>(null)

const storageKey = 'clavis.org'

export function OrgProvider({ children }: { children: ReactNode }) {
  const { orgs } = useAuth()
  const [slug, setSlug] = useState(() => localStorage.getItem(storageKey) ?? '')
  const org = orgs.find((candidate) => candidate.slug === slug) ?? orgs[0]

  useEffect(() => {
    if (org && org.slug !== slug) {
      localStorage.setItem(storageKey, org.slug)
      setSlug(org.slug)
    }
  }, [org, slug])

  const selectOrg = useCallback((next: string) => {
    localStorage.setItem(storageKey, next)
    setSlug(next)
    window.dispatchEvent(new CustomEvent('clavis:org-changed'))
  }, [])

  const value = useMemo<OrgContextValue>(() => ({ org, orgs, selectOrg }), [org, orgs, selectOrg])
  return <OrgContext.Provider value={value}>{children}</OrgContext.Provider>
}

export function useCurrentOrg() {
  const value = useContext(OrgContext)
  if (!value) throw new Error('useCurrentOrg must be used inside OrgProvider')
  return value
}
