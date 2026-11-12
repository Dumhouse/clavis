import { onMounted, ref } from 'vue'

export const REPO = 'dumhouse/clavis'
export const REPO_URL = `https://github.com/${REPO}`

type Stats = { stars: number | null; release: string | null; releaseUrl: string | null }

const state = ref<Stats>({ stars: null, release: null, releaseUrl: null })
let loading: Promise<void> | null = null
const CACHE_KEY = 'clavis.gh-stats'
const TTL = 60 * 60 * 1000

/*
 * Stars and the latest release, fetched from the public GitHub API once per
 * hour per visitor. Anything that fails (private repo, rate limit, offline)
 * leaves the values null and the UI simply hides them.
 */
async function load() {
  try {
    const cached = JSON.parse(sessionStorage.getItem(CACHE_KEY) || 'null')
    if (cached && Date.now() - cached.at < TTL) {
      state.value = cached.data
      return
    }
  } catch {
    /* ignore bad cache */
  }
  const get = (path: string) =>
    fetch(`https://api.github.com/repos/${REPO}${path}`, {
      headers: { Accept: 'application/vnd.github+json' },
    }).then((r) => (r.ok ? r.json() : null))
  const [repo, release] = await Promise.all([get(''), get('/releases/latest')]).catch(() => [null, null])
  const data: Stats = {
    stars: typeof repo?.stargazers_count === 'number' ? repo.stargazers_count : null,
    release: release?.tag_name ?? null,
    releaseUrl: release?.html_url ?? null,
  }
  state.value = data
  try {
    sessionStorage.setItem(CACHE_KEY, JSON.stringify({ at: Date.now(), data }))
  } catch {
    /* storage full or disabled */
  }
}

export function formatCount(n: number) {
  return n >= 1000 ? `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k` : String(n)
}

export function useGitHub() {
  onMounted(() => {
    loading ??= load()
  })
  return state
}
