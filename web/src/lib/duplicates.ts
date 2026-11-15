// Finds repeated secret values across environments, in the browser, from the
// values the compare view has already loaded. Nothing derived from a value is
// ever shown: the results name keys and environments only.

export interface DuplicateSecret {
  key: string
  kind?: string | undefined
  value?: string | null | undefined
  inherited?: boolean | undefined
}

export interface DuplicateEnv {
  slug: string
  name: string
  type?: string | undefined
  secrets: DuplicateSecret[]
}

export interface SameValueGroup {
  env: string
  keys: string[]
}

export interface SharedKeyGroup {
  key: string
  envs: string[]
  withProduction: boolean
}

export interface EnvPair {
  a: string
  b: string
  common: number
  same: number
  onlyA: number
  onlyB: number
  keyPercent: number
  valuePercent: number
}

/** The shortest value worth comparing: "true" or "3000" repeat everywhere. */
export const MIN_DUPLICATE_LENGTH = 6

function comparable(secret: DuplicateSecret): string | null {
  const value = secret.value ?? ''
  if (secret.inherited || secret.kind !== 'secret' || value.length < MIN_DUPLICATE_LENGTH) return null
  return value
}

export function findDuplicates(envs: DuplicateEnv[]): {
  sameValue: SameValueGroup[]
  sharedKey: SharedKeyGroup[]
} {
  const sameValue: SameValueGroup[] = []
  const across = new Map<string, Map<string, Set<string>>>()
  const production = new Set(envs.filter((env) => env.type === 'production').map((env) => env.slug))
  for (const env of envs) {
    const byValue = new Map<string, string[]>()
    for (const secret of env.secrets) {
      const value = comparable(secret)
      if (value === null) continue
      byValue.set(value, [...(byValue.get(value) ?? []), secret.key])
      const digests = across.get(secret.key) ?? new Map<string, Set<string>>()
      const where = digests.get(value) ?? new Set<string>()
      where.add(env.slug)
      digests.set(value, where)
      across.set(secret.key, digests)
    }
    for (const keys of byValue.values()) {
      if (keys.length > 1) sameValue.push({ env: env.slug, keys: [...keys].sort() })
    }
  }
  const sharedKey: SharedKeyGroup[] = []
  for (const [key, digests] of across) {
    for (const where of digests.values()) {
      if (where.size < 2) continue
      const slugs = [...where].sort()
      sharedKey.push({ key, envs: slugs, withProduction: slugs.some((slug) => production.has(slug)) })
    }
  }
  sameValue.sort((x, y) => x.env.localeCompare(y.env) || (x.keys[0] ?? '').localeCompare(y.keys[0] ?? ''))
  sharedKey.sort(
    (x, y) => Number(y.withProduction) - Number(x.withProduction) || x.key.localeCompare(y.key),
  )
  return { sameValue, sharedKey }
}

export function similarEnvironments(envs: DuplicateEnv[]): EnvPair[] {
  const own = envs.map(
    (env) => new Map(env.secrets.filter((s) => !s.inherited).map((s) => [s.key, s.value ?? ''])),
  )
  const pairs: EnvPair[] = []
  for (const [i, first] of envs.entries()) {
    for (const [j, second] of envs.entries()) {
      if (j <= i) continue
      const left = own[i]
      const right = own[j]
      if (!left || !right) continue
      let common = 0
      let same = 0
      let onlyA = 0
      for (const [key, value] of left) {
        if (!right.has(key)) onlyA++
        else {
          common++
          if (right.get(key) === value) same++
        }
      }
      let onlyB = 0
      for (const key of right.keys()) if (!left.has(key)) onlyB++
      const all = common + onlyA + onlyB
      pairs.push({
        a: first.slug,
        b: second.slug,
        common,
        same,
        onlyA,
        onlyB,
        keyPercent: all ? Math.floor((common * 100) / all) : 0,
        valuePercent: common ? Math.floor((same * 100) / common) : 0,
      })
    }
  }
  return pairs.sort((x, y) => y.valuePercent - x.valuePercent || y.keyPercent - x.keyPercent)
}
