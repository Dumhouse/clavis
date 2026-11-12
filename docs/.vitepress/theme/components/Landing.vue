<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { withBase } from 'vitepress'

import HeroStack from './mock/HeroStack.vue'
import CoverageMock from './mock/CoverageMock.vue'
import SchemaMock from './mock/SchemaMock.vue'
import AccessMock from './mock/AccessMock.vue'
import AgentFlow from './mock/AgentFlow.vue'
import { REPO_URL, formatCount, useGitHub } from '../github'
import { faq } from '../../faq'

import secretsDark from '../../../assets/screenshots/secrets-dark.jpg'
import secretsLight from '../../../assets/screenshots/secrets-light.jpg'
import promoteDark from '../../../assets/screenshots/promote.jpg'
import promoteLight from '../../../assets/screenshots/promote-light.jpg'
import schemaDark from '../../../assets/screenshots/schema.jpg'
import schemaLight from '../../../assets/screenshots/schema-light.jpg'
import agentsDark from '../../../assets/screenshots/agents.jpg'
import agentsLight from '../../../assets/screenshots/agents-light.jpg'
import auditDark from '../../../assets/screenshots/audit.jpg'
import auditLight from '../../../assets/screenshots/audit-light.jpg'

const repo = REPO_URL
const gh = useGitHub()
const dockerCmd = 'docker run -p 8080:8080 -v clavis-data:/data ghcr.io/dumhouse/clavis:latest'

const copied = ref(false)
async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = true
    setTimeout(() => (copied.value = false), 1600)
  } catch {
    /* clipboard unavailable: ignore */
  }
}

const integrations = ['GitHub Actions', 'GitLab CI', 'Kubernetes', 'AWS', 'Google Cloud', 'Vercel', 'Cloudflare', 'Claude Code', 'Cursor', 'Codex']

/* Product blocks ---------------------------------------------------------- */

const blocks = [
  {
    id: 'secrets',
    tag: 'Secrets management',
    text: 'Every environment side by side, with inheritance, versioning, and a typed contract that tells you what is missing before you deploy.',
    link: '/quickstart',
    cta: 'Explore secrets',
    items: [
      { label: 'Environments + inheritance', text: 'Development, staging, production, branch environments, and regional overlays without copy-paste.', link: '/commands#environments' },
      { label: 'Versioning + rollback', text: 'Every change is an immutable version. Rollback creates a new one instead of rewriting history.', link: '/commands#clavis-secrets-history-key' },
      { label: 'Promotion + snapshots', text: 'Diff two environments, tick the keys to copy, and snapshot the target first. Protected targets wait for approval.', link: '/commands#clavis-promote' },
    ],
  },
  {
    id: 'schema',
    tag: 'Typed config',
    text: 'Declare types, patterns, enums, and where each key is required. Commit the schema next to your code and let CI reject bad values.',
    link: '/commands#schema-and-validation',
    cta: 'Explore the schema',
    items: [
      { label: 'Types + patterns', text: 'url, int, bool, json, enum, and regular expressions, per key and per environment type.', link: '/commands#clavis-schema-ls' },
      { label: 'Generated loaders', text: 'zod for TypeScript, a struct for Go, pydantic for Python. Your app fails fast on bad config.', link: '/commands#clavis-codegen' },
      { label: 'CI gate', text: 'clavis check exits 6 on any missing or invalid value, so drift never reaches production.', link: '/commands#clavis-check' },
    ],
  },
  {
    id: 'access',
    tag: 'Access + dynamic secrets',
    text: 'Grant access for an hour, not forever. Issue database and cloud credentials that expire on their own.',
    link: '/security',
    cta: 'Explore access',
    items: [
      { label: 'JIT access + approvals', text: 'Request a role for a limited time with a reason. An Admin or Owner approves, and it is all audited.', link: '/security' },
      { label: 'Dynamic credentials', text: 'Short-lived PostgreSQL roles and AWS STS credentials that renew, expire, and revoke when a command exits.', link: '/commands#dynamic-secrets' },
      { label: 'Rotation + expiry', text: 'Rotation schedules, expiry reminders, and health findings for stale or weak secrets.', link: '/security' },
    ],
  },
]

/* Workflow tabs ----------------------------------------------------------- */

type Line = { t: 'cmd' | 'out' | 'cmt' | 'ok' | 'warn'; s: string }
const flows: { id: string; label: string; body: string; link: string; lines: Line[] }[] = [
  {
    id: 'local',
    label: 'Local dev',
    body: 'Sign in once, link the repo, and run any command with values in its environment. Output is masked, and --watch restarts the process when a secret changes.',
    link: '/commands#clavis-run-command-args',
    lines: [
      { t: 'cmd', s: 'clavis login' },
      { t: 'ok', s: '✓ Signed in as ada@acme.dev (token stored in OS keychain)' },
      { t: 'cmd', s: 'clavis link --project web-app --env development' },
      { t: 'ok', s: '✓ Wrote .clavis.yaml' },
      { t: 'cmd', s: 'clavis run --watch -- pnpm dev' },
      { t: 'out', s: '  ready  DATABASE_URL=postgres://app:******@db:5432/app' },
      { t: 'out', s: '  ready  STRIPE_SECRET_KEY=******' },
    ],
  },
  {
    id: 'schema',
    label: 'Typed config',
    body: 'Infer a schema from what you have, generate a typed loader, and fail CI on drift.',
    link: '/commands#schema-and-validation',
    lines: [
      { t: 'cmd', s: 'clavis schema infer -o clavis.schema.yaml' },
      { t: 'cmd', s: 'clavis codegen --lang ts -o src/config.ts' },
      { t: 'ok', s: '✓ Generated zod schema, Config type, and loadConfig()' },
      { t: 'cmd', s: 'clavis check --env production' },
      { t: 'warn', s: '✕ SENTRY_DSN  required in production, missing' },
      { t: 'warn', s: '✕ LOG_LEVEL   "verbose" is not one of debug|info|warn|error' },
      { t: 'out', s: '  exit status 6' },
    ],
  },
  {
    id: 'agents',
    label: 'AI agents',
    body: 'Create an agent identity, allow exact commands, and register the MCP server. The config holds no secret.',
    link: '/agents',
    lines: [
      { t: 'cmd', s: 'clavis identities create claude-code --type agent --project web-app' },
      { t: 'cmd', s: 'clavis agents allow claude-code "go test ./..." "pnpm test"' },
      { t: 'cmd', s: 'claude mcp add clavis -- clavis mcp --agent claude-code' },
      { t: 'ok', s: '✓ run_with_secrets("pnpm test")   allowed, output masked' },
      { t: 'warn', s: '✕ run_with_secrets("env")         denied by allowlist, audited' },
    ],
  },
  {
    id: 'ci',
    label: 'CI',
    body: 'Machine tokens are scoped to a project and environment. Workload OIDC from GitHub Actions and GitLab avoids long-lived tokens.',
    link: '/ci',
    lines: [
      { t: 'cmt', s: '# .github/workflows/test.yml' },
      { t: 'out', s: '  env:' },
      { t: 'out', s: '    CLAVIS_TOKEN: ${{ secrets.CLAVIS_TOKEN }}' },
      { t: 'out', s: '    CLAVIS_SERVER: https://clavis.acme.dev' },
      { t: 'cmd', s: 'clavis check --env staging' },
      { t: 'cmd', s: 'clavis --env staging run -- pnpm test' },
      { t: 'ok', s: '✓ 214 tests passed' },
    ],
  },
]
const flowIdx = ref(0)
const flow = computed(() => flows[flowIdx.value])

/*
 * Type commands character by character and reveal output lines after them.
 * Starts when the terminal scrolls into view; reduced motion shows it all.
 */
const term = ref<HTMLElement | null>(null)
const lineIdx = ref(Infinity)
const charIdx = ref(0)
let typeTimer: ReturnType<typeof setTimeout> | undefined
let termObserver: IntersectionObserver | undefined
let reduceMotion = false
let started = false

function shown(i: number) {
  const l = flow.value.lines[i]
  if (i < lineIdx.value) return l.s
  if (i === lineIdx.value && l.t === 'cmd') return l.s.slice(0, charIdx.value)
  return null
}

function step() {
  const lines = flow.value.lines
  if (lineIdx.value >= lines.length) return
  const l = lines[lineIdx.value]
  if (l.t === 'cmd' && charIdx.value < l.s.length) {
    charIdx.value++
    typeTimer = setTimeout(step, 22 + Math.random() * 30)
    return
  }
  lineIdx.value++
  charIdx.value = 0
  const next = lines[lineIdx.value]
  typeTimer = setTimeout(step, !next ? 0 : next.t === 'cmd' ? 420 : 160)
}

function play() {
  clearTimeout(typeTimer)
  if (reduceMotion) {
    lineIdx.value = Infinity
    return
  }
  lineIdx.value = 0
  charIdx.value = 0
  typeTimer = setTimeout(step, 300)
}

watch(flowIdx, () => started && play())

onMounted(() => {
  reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  if (reduceMotion || !term.value) return
  lineIdx.value = 0
  termObserver = new IntersectionObserver(([e]) => {
    if (e.isIntersecting && !started) {
      started = true
      play()
      termObserver?.disconnect()
    }
  }, { threshold: 0.4 })
  termObserver.observe(term.value)
})

onBeforeUnmount(() => {
  clearTimeout(typeTimer)
  termObserver?.disconnect()
})

/* Product tour (real screenshots, swapped by theme) ----------------------- */

const tour = [
  { label: 'Secrets', dark: secretsDark, light: secretsLight, text: 'Every environment side by side; each reveal is audited with a reason.' },
  { label: 'Promotion', dark: promoteDark, light: promoteLight, text: 'Added, changed, and missing keys before anything is written.' },
  { label: 'Schema', dark: schemaDark, light: schemaLight, text: 'Types, patterns, enums, and required environments.' },
  { label: 'Agents', dark: agentsDark, light: agentsLight, text: 'Agent identities, expiry, and deny-all allowlists.' },
  { label: 'Audit', dark: auditDark, light: auditLight, text: 'A hash-chained audit log with no secret values.' },
]
const tourIdx = ref(0)

const more = [
  'Encrypted share links', 'Syncs to GitHub, Vercel, AWS, GCP, Netlify, Cloudflare, Vault', 'Importers for Infisical, Doppler, Vault, 1Password, .env',
  'Kubernetes operator + CSI driver', 'External Secrets webhook', 'OIDC + SAML SSO, SCIM', 'PostgreSQL HA', 'Encrypted scheduled backups',
  'Secret scanning', 'Offline cache', 'OpenAPI 3.1', 'Command palette',
]

// Checked against each vendor's public pricing and docs, November 2026.
// 'yes' renders a check, 'no' a cross; anything else is shown as text.
const compare = [
  ['License', 'Apache-2.0', 'BSL 1.1', 'MIT core + paid', 'Proprietary'],
  ['Self-hosting', 'yes', 'yes', 'yes', 'Enterprise'],
  ['Single binary with embedded UI', 'yes', 'yes', 'no', 'no'],
  ['SSO and RBAC', 'yes', 'yes', 'Paid', 'Paid'],
  ['Dynamic database and cloud credentials', 'yes', 'yes', 'Paid', 'Enterprise'],
  ['Time-limited access and approvals', 'yes', 'Enterprise', 'Paid', 'Paid'],
  ['Typed schema', 'yes', 'no', 'no', 'Value types'],
  ['Generated typed config loaders', 'yes', 'no', 'no', 'no'],
  ['MCP server', 'yes', 'Beta', 'yes', 'yes'],
  ['Agents run commands without seeing values', 'yes', 'no', 'Separate product', 'no'],
  ['Expiring share links', 'yes', 'no', 'yes', 'yes'],
]

const security = [
  { title: 'Envelope encryption', text: 'Values are encrypted with a per-project key before they reach the database.' },
  { title: 'Separate root key', text: 'AWS KMS, GCP KMS, a 0600 file, or Shamir shares — never next to the data.' },
  { title: 'Default-deny routes', text: 'Every API route declares the permission it needs; undeclared routes fail closed.' },
  { title: 'Signed, reproducible releases', text: 'cosign keyless signatures, SBOMs, and builds you can reproduce byte for byte.' },
]
</script>

<template>
  <div class="landing">
    <!-- Hero -->
    <section class="hero rails">
      <div class="container hero-grid">
        <div class="hero-copy">
          <a v-if="gh.release" class="pill" :href="gh.releaseUrl || repo">
            <span class="badge">New</span>
            Clavis {{ gh.release }} is out — see what changed
            <span class="arrow">›</span>
          </a>
          <a v-else class="pill" :href="repo">
            <span class="badge">Open source</span>
            Apache-2.0 · every feature free
            <span class="arrow">›</span>
          </a>
          <h1>
            Secrets management for <mark>humans</mark>, machines, and <mark>AI agents</mark>
          </h1>
          <p class="lede">
            One Go binary with an embedded UI. Typed config, dynamic credentials, JIT access, and an
            auditable path for coding agents. No unseal ceremony, no enterprise edition.
          </p>
          <div class="actions">
            <a class="btn primary" :href="withBase('/quickstart')">› Get started</a>
            <a class="btn ghost" :href="repo">
              <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true"><path fill="currentColor" d="M12 .5a11.5 11.5 0 0 0-3.64 22.41c.58.1.79-.25.79-.56v-2c-3.2.7-3.880-1.37-3.88-1.37-.53-1.33-1.28-1.69-1.28-1.69-1.05-.71.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.030 1.77 2.71 1.26 3.37.96.1-.75.4-1.26.73-1.55-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.19-3.1-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.84 1.19 3.1 0 4.42-2.7 5.39-5.26 5.68.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .5Z" /></svg>
              Star on GitHub
              <span v-if="gh.stars !== null" class="count">{{ formatCount(gh.stars) }}</span>
            </a>
          </div>
          <button class="install" type="button" :aria-label="'Copy: ' + dockerCmd" @click="copy(dockerCmd)">
            <span class="prompt">$</span>
            <code>{{ dockerCmd }}</code>
            <span class="copy">{{ copied ? 'Copied' : 'Copy' }}</span>
          </button>
        </div>
        <HeroStack class="hero-mock" />
      </div>
    </section>

    <!-- Works with -->
    <section class="works rails">
      <div class="container works-inner">
        <span class="works-label">Works with</span>
        <span v-for="name in integrations" :key="name" class="works-item">{{ name }}</span>
      </div>
    </section>

    <!-- Intro -->
    <section class="intro rails">
      <div class="container">
        <h2>One binary for <mark>every</mark> secret your apps, pipelines, and agents touch.</h2>
        <p>Secrets, typed configuration, dynamic credentials, and access control in one place, with one audit trail for every person, machine, and agent.</p>
        <a class="btn ghost small" :href="withBase('/architecture')">› How it works</a>
      </div>
    </section>

    <!-- Product blocks -->
    <section v-for="b in blocks" :key="b.id" class="block rails">
      <div class="container">
        <div class="block-head">
          <div>
            <h3><mark>{{ b.tag }}</mark></h3>
            <p>{{ b.text }}</p>
          </div>
          <a class="btn ghost small" :href="withBase(b.link)">› {{ b.cta }}</a>
        </div>
        <div class="block-body">
          <ul class="items">
            <li v-for="it in b.items" :key="it.label">
              <span class="item-label">{{ it.label }}</span>
              <p>{{ it.text }}</p>
              <a :href="withBase(it.link)">› Learn more</a>
            </li>
          </ul>
          <div class="m-stage stage">
            <CoverageMock v-if="b.id === 'secrets'" />
            <SchemaMock v-else-if="b.id === 'schema'" />
            <AccessMock v-else />
          </div>
        </div>
      </div>
    </section>

    <!-- Agents (always dark) -->
    <section class="agents">
      <div class="container">
        <div class="agents-head">
          <h2>Agents shouldn’t see your secrets.<br /><span>Let them use secrets without reading them.</span></h2>
          <a class="btn light small" :href="withBase('/agents')">› Agent setup</a>
        </div>
        <AgentFlow />
        <div class="agents-points">
          <div>
            <strong>Deny-all allowlist</strong>
            <p>Each agent has its own identity and only runs the exact commands you allow. Production is out of scope.</p>
          </div>
          <div>
            <strong>Values never returned</strong>
            <p>MCP tools list key names and run commands. No tool returns a value, and output is masked.</p>
          </div>
          <div>
            <strong>Audited before decryption</strong>
            <p>The command text is written to the audit chain before any value is decrypted, allowed or denied.</p>
          </div>
        </div>
        <p class="agents-note">
          Clavis protects what reaches the agent through MCP; pair it with your agent’s sandbox.
          <a :href="withBase('/agents#what-this-protects-and-what-it-does-not')">What this does not protect →</a>
        </p>
      </div>
    </section>

    <!-- Workflows -->
    <section class="section rails">
      <div class="container">
        <header class="head">
          <h2>From laptop to production, <mark>one CLI</mark></h2>
          <p>The same binary runs the server and the CLI, so the commands you use locally are the ones CI and agents use.</p>
        </header>
        <div class="flow">
          <div class="flow-tabs" role="tablist" aria-label="Workflows">
            <button v-for="(f, i) in flows" :key="f.id" role="tab" type="button" :aria-selected="i === flowIdx" :class="{ active: i === flowIdx }" @click="flowIdx = i">{{ f.label }}</button>
          </div>
          <div ref="term" class="term">
            <div class="term-bar"><span /><span /><span /><em>{{ flow.label.toLowerCase() }} — zsh</em></div>
            <pre :aria-label="flow.lines.map((l) => l.s).join('\n')"><template v-for="(l, i) in flow.lines" :key="flow.id + i"><span v-if="shown(i) !== null" :class="'l-' + l.t" aria-hidden="true"><template v-if="l.t === 'cmd'"><b>$ </b></template>{{ shown(i) }}<i v-if="i === lineIdx" class="caret" /></span><template v-if="shown(i) !== null">
</template></template><span v-if="lineIdx >= flow.lines.length" aria-hidden="true"><b>$ </b><i class="caret" /></span></pre>
          </div>
          <div class="flow-copy">
            <p>{{ flow.body }}</p>
            <a :href="withBase(flow.link)">› Read the guide</a>
          </div>
        </div>
      </div>
    </section>

    <!-- Tour -->
    <section class="section rails">
      <div class="container">
        <header class="head">
          <h2>A UI that <mark>explains itself</mark></h2>
          <p>Real screens from a demo instance, in whichever theme you are reading this.</p>
        </header>
        <div class="tour-tabs" role="tablist" aria-label="Screens">
          <button v-for="(s, i) in tour" :key="s.label" role="tab" type="button" :aria-selected="i === tourIdx" :class="{ active: i === tourIdx }" @click="tourIdx = i">{{ s.label }}</button>
        </div>
        <div class="m-stage tour-stage">
          <figure class="tour-shot">
            <img class="only-dark" :src="tour[tourIdx].dark" :alt="tour[tourIdx].label + ' screen'" />
            <img class="only-light" :src="tour[tourIdx].light" :alt="tour[tourIdx].label + ' screen'" />
          </figure>
        </div>
        <p class="tour-caption">{{ tour[tourIdx].text }} <a :href="withBase('/screenshots')">See every screen →</a></p>
      </div>
    </section>

    <!-- More -->
    <section class="section rails">
      <div class="container">
        <header class="head">
          <h2>Everything included. <mark>No paid tier.</mark></h2>
          <p>SSO, RBAC, approvals, rotation, and dynamic secrets ship in the same Apache-2.0 build as everything else.</p>
        </header>
        <div class="more">
          <span v-for="m in more" :key="m"><i>✓</i>{{ m }}</span>
        </div>
      </div>
    </section>

    <!-- Comparison -->
    <section class="section rails">
      <div class="container narrow">
        <header class="head">
          <h2>How Clavis compares</h2>
          <p>From each vendor’s public pricing and docs, checked November 2026. “Paid” and “Enterprise” mean the feature exists but not on the free plan.</p>
        </header>
        <div class="table-scroll">
          <table class="cmp">
            <thead>
              <tr><th /><th class="us">Clavis</th><th>Vault</th><th>Infisical</th><th>Doppler</th></tr>
            </thead>
            <tbody>
              <tr v-for="row in compare" :key="row[0]">
                <td>{{ row[0] }}</td>
                <td v-for="(c, j) in row.slice(1)" :key="j" :class="{ us: j === 0 }">
                  <span v-if="c === 'yes'" class="yes" aria-label="Yes">✓</span>
                  <span v-else-if="c === 'no'" class="no" aria-label="No">✕</span>
                  <span v-else class="partial" :class="{ strong: j === 0 }">{{ c }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </section>

    <!-- Security + self-host -->
    <section class="section rails">
      <div class="container split">
        <div>
          <h3><mark>Security you can inspect</mark></h3>
          <p class="muted">The design and threat model are public, and every claim links to code or docs.</p>
          <ul class="sec-list">
            <li v-for="s in security" :key="s.title"><strong>{{ s.title }}</strong><span>{{ s.text }}</span></li>
          </ul>
          <a class="link" :href="withBase('/security')">› Read the security design</a>
        </div>
        <div>
          <h3><mark>Running in five minutes</mark></h3>
          <p class="muted">SQLite by default and a browser setup wizard. Add PostgreSQL, KMS, and SSO when you need them.</p>
          <ol class="steps">
            <li><strong>Start the binary</strong><span>Clavis prints a one-time setup token and writes it to the data directory.</span></li>
            <li><strong>Claim the instance</strong><span>Paste the token, create the owner account and organization. Setup then closes for good.</span></li>
            <li><strong>Run your app</strong><span>Install the CLI, sign in once, link the repo, and use <code>clavis run</code>.</span></li>
          </ol>
          <a class="link" :href="withBase('/self-hosting')">› Production deployment guide</a>
        </div>
      </div>
    </section>

    <!-- FAQ -->
    <section class="section rails">
      <div class="container narrow">
        <header class="head">
          <h2>Questions, <mark>answered</mark></h2>
          <p>Can’t find yours? Ask in <a :href="repo + '/discussions'">GitHub Discussions</a>.</p>
        </header>
        <div class="faq">
          <details v-for="(f, i) in faq" :key="f.q" :open="i === 0">
            <summary>{{ f.q }}<span class="plus" aria-hidden="true" /></summary>
            <p>
              {{ f.a }}
              <a v-if="f.link" :href="withBase(f.link.href)">{{ f.link.text }} →</a>
            </p>
          </details>
        </div>
      </div>
    </section>

    <!-- CTA -->
    <section class="section rails last">
      <div class="container">
        <div class="final m-stage">
          <div>
            <img :src="withBase('/brand/logo-mark.svg')" alt="" width="44" height="44" />
            <h2>Own your secrets again</h2>
            <p>Free, open source, and yours to run.</p>
            <div class="actions">
              <a class="btn primary" :href="withBase('/quickstart')">› Read the quickstart</a>
              <a class="btn ghost" :href="repo">View on GitHub</a>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.landing {
  color: var(--vp-c-text-1);
  overflow-x: clip;
}

.container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 40px;
}

.container.narrow > * {
  max-width: 920px;
  margin-inline: auto;
}

/* Vertical rails + section dividers give the page its blueprint grid. */
.rails {
  position: relative;
  border-bottom: 1px solid var(--c-line);
}

.rails > .container {
  border-inline: 1px solid var(--c-line);
}

mark {
  padding: 0 0.12em;
  border-radius: 3px;
  background: var(--c-mark);
  color: inherit;
  -webkit-box-decoration-break: clone;
  box-decoration-break: clone;
}

/* Hero */
.hero {
  background-image:
    linear-gradient(var(--c-grid) 1px, transparent 1px),
    linear-gradient(90deg, var(--c-grid) 1px, transparent 1px);
  background-size: 40px 40px;
  background-position: center top;
}

.hero > .container {
  background: linear-gradient(90deg, var(--vp-c-bg) 40%, transparent);
}

.hero-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.05fr);
  gap: 56px;
  align-items: center;
  padding-top: 88px;
  padding-bottom: 96px;
}

.pill {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 4px 12px 4px 4px;
  border: 1px solid var(--c-line);
  border-radius: 6px;
  background: var(--c-panel);
  color: var(--vp-c-text-2);
  font-size: 13px;
  text-decoration: none;
}

.pill:hover {
  border-color: var(--c-line-strong);
  color: var(--vp-c-text-1);
}

.pill .badge {
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--c-mark);
  color: var(--vp-c-text-1);
  font-weight: 600;
  font-size: 12px;
}

h1 {
  margin: 24px 0 0;
  font-size: clamp(2.4rem, 4.6vw, 3.6rem);
  line-height: 1.08;
  letter-spacing: -0.04em;
  font-weight: 650;
}

.lede {
  margin: 22px 0 0;
  max-width: 540px;
  font-size: 1.08rem;
  line-height: 1.65;
  color: var(--vp-c-text-2);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 30px;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 42px;
  padding: 0 18px;
  border-radius: 6px;
  font-weight: 600;
  font-size: 14.5px;
  text-decoration: none;
  white-space: nowrap;
  transition: background 0.15s, border-color 0.15s;
}

.btn.small {
  height: 36px;
  padding: 0 14px;
  font-size: 13.5px;
}

.btn.primary {
  background: var(--c-btn-bg);
  color: var(--c-btn-text);
}

.btn.primary:hover {
  background: var(--c-btn-hover);
}

.btn.ghost {
  border: 1px solid var(--c-line-strong);
  background: var(--c-panel);
  color: var(--vp-c-text-1);
}

.btn.ghost:hover {
  background: var(--c-panel-3);
}

.btn .count {
  margin-left: 2px;
  padding: 1px 7px;
  border-radius: 4px;
  background: var(--c-panel-3);
  font-size: 12px;
}

.btn.light {
  background: #f4f4f5;
  color: #0a0a0c;
}

.btn.light:hover {
  background: #fff;
}

.install {
  display: flex;
  align-items: center;
  gap: 12px;
  max-width: 100%;
  margin-top: 22px;
  padding: 8px 8px 8px 14px;
  border: 1px solid var(--c-line);
  border-radius: 8px;
  background: var(--c-panel-2);
  font-family: var(--vp-font-family-mono);
  font-size: 12.5px;
  cursor: pointer;
  text-align: left;
}

.install:hover {
  border-color: var(--c-line-strong);
}

.install .prompt {
  color: var(--c-accent-text);
}

.install code {
  overflow-x: auto;
  white-space: nowrap;
  color: var(--vp-c-text-1);
  background: none;
  padding: 0;
}

.install .copy {
  flex: none;
  padding: 4px 10px;
  border-radius: 5px;
  border: 1px solid var(--c-line);
  background: var(--c-panel);
  color: var(--vp-c-text-2);
  font-family: var(--vp-font-family-base);
  font-size: 12px;
}

/* Works with */
.works-inner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 12px 30px;
  padding-block: 26px;
}

.works-label {
  font-family: var(--vp-font-family-mono);
  font-size: 11px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--vp-c-text-3);
}

.works-item {
  font-weight: 600;
  font-size: 15px;
  letter-spacing: -0.01em;
  color: var(--vp-c-text-2);
}

/* Intro */
.intro .container {
  padding-top: 96px;
  padding-bottom: 72px;
  text-align: center;
}

.intro h2 {
  max-width: 760px;
  margin: 0 auto;
}

.intro p {
  max-width: 620px;
  margin: 18px auto 26px;
  color: var(--vp-c-text-2);
  line-height: 1.65;
}

h2 {
  margin: 0;
  font-size: clamp(1.75rem, 3.2vw, 2.4rem);
  line-height: 1.15;
  letter-spacing: -0.035em;
  font-weight: 600;
}

h3 {
  margin: 0;
  font-size: 1.5rem;
  letter-spacing: -0.025em;
  font-weight: 600;
}

/* Product blocks */
.block .container {
  padding-top: 64px;
  padding-bottom: 64px;
}

.block-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 28px;
}

.block-head p {
  max-width: 600px;
  margin: 12px 0 0;
  color: var(--vp-c-text-2);
  line-height: 1.65;
}

.block-body {
  display: grid;
  grid-template-columns: 330px minmax(0, 1fr);
  border: 1px solid var(--c-line);
  border-radius: 4px;
  overflow: hidden;
  background: var(--c-panel);
}

.items {
  list-style: none;
  margin: 0;
  padding: 0;
  border-right: 1px solid var(--c-line);
}

.items li {
  padding: 22px 24px;
  border-bottom: 1px solid var(--c-line);
}

.items li:last-child {
  border-bottom: 0;
}

.item-label {
  font-family: var(--vp-font-family-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--vp-c-text-3);
}

.items p {
  margin: 8px 0 10px;
  font-size: 14px;
  line-height: 1.6;
  color: var(--vp-c-text-1);
}

.items a {
  font-size: 13px;
  font-weight: 500;
  color: var(--c-accent-text);
  text-decoration: none;
}

.items a:hover {
  text-decoration: underline;
}

.stage {
  display: grid;
  align-items: center;
  padding: 40px 40px 40px 48px;
  min-width: 0;
}

/* Agents */
.agents {
  background: #0a0a0c;
  color: #f4f4f5;
  border-bottom: 1px solid #1f1f25;
}

.agents .container {
  padding-top: 96px;
  padding-bottom: 88px;
}

.agents-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 56px;
}

.agents-head h2 span {
  color: #8b8b95;
}

.agents-points {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 32px;
  margin-top: 64px;
}

.agents-points strong {
  font-size: 15px;
}

.agents-points p {
  margin: 8px 0 0;
  color: #a1a1aa;
  font-size: 14px;
  line-height: 1.6;
}

.agents-note {
  margin: 40px 0 0;
  padding-top: 20px;
  border-top: 1px solid #1f1f25;
  color: #8b8b95;
  font-size: 13.5px;
}

.agents-note a {
  color: #6ee7b7;
}

/* Generic sections */
.section .container {
  padding-top: 88px;
  padding-bottom: 88px;
}

.section.last .container {
  padding-bottom: 96px;
}

.head {
  max-width: 680px;
  margin: 0 auto 40px;
  text-align: center;
}

.head p,
.muted {
  margin: 14px 0 0;
  color: var(--vp-c-text-2);
  line-height: 1.65;
}

/* Workflows */
.flow {
  max-width: 900px;
  margin: 0 auto;
}

.flow-tabs,
.tour-tabs {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 4px;
  width: fit-content;
  margin: 0 auto 20px;
  padding: 4px;
  border: 1px solid var(--c-line);
  border-radius: 8px;
  background: var(--c-panel-2);
}

.flow-tabs button,
.tour-tabs button {
  padding: 7px 14px;
  border-radius: 5px;
  font-size: 13.5px;
  font-weight: 500;
  color: var(--vp-c-text-2);
  cursor: pointer;
}

.flow-tabs button:hover,
.tour-tabs button:hover {
  color: var(--vp-c-text-1);
}

.flow-tabs button.active,
.tour-tabs button.active {
  background: var(--c-panel);
  color: var(--vp-c-text-1);
  box-shadow: 0 0 0 1px var(--c-line-strong), 0 1px 2px rgb(0 0 0 / 0.06);
}

.term {
  border: 1px solid #2a2a31;
  border-radius: 10px;
  background: #0b0b0e;
  overflow: hidden;
  box-shadow: var(--c-shadow-lg);
}

.term-bar {
  display: flex;
  align-items: center;
  gap: 7px;
  height: 36px;
  padding: 0 14px;
  border-bottom: 1px solid #1c1c22;
  background: #101014;
}

.term-bar > span {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  background: #2a2a31;
}

.term-bar em {
  margin-left: 10px;
  font-style: normal;
  font-size: 12px;
  color: #71717a;
}

.term pre {
  margin: 0;
  padding: 20px 22px;
  min-height: 236px;
  overflow-x: auto;
  font-family: var(--vp-font-family-mono);
  font-size: 13.5px;
  line-height: 1.85;
}

.caret {
  display: inline-block;
  width: 8px;
  height: 1.05em;
  margin-left: 1px;
  vertical-align: -0.15em;
  background: #34d399;
  animation: blink 1s steps(1) infinite;
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

.term b {
  color: #34d399;
  font-weight: 400;
}

.l-cmd { color: #f4f4f5; }
.l-out { color: #a1a1aa; }
.l-cmt { color: #71717a; }
.l-ok { color: #6ee7b7; }
.l-warn { color: #fca5a5; }

.flow-copy {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px 24px;
  margin-top: 18px;
}

.flow-copy p {
  flex: 1 1 420px;
  margin: 0;
  color: var(--vp-c-text-2);
  line-height: 1.65;
  font-size: 14.5px;
}

.flow-copy a,
.link,
.tour-caption a {
  color: var(--c-accent-text);
  font-weight: 600;
  font-size: 14px;
  text-decoration: none;
}

.flow-copy a:hover,
.link:hover,
.tour-caption a:hover {
  text-decoration: underline;
}

/* Tour */
.tour-stage {
  padding: 48px 48px 0;
  border: 1px solid var(--c-line);
  border-radius: 4px;
}

.tour-shot {
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--c-line-strong);
  border-bottom: 0;
  border-radius: 10px 10px 0 0;
  box-shadow: var(--c-shadow-lg);
  background: var(--c-panel);
}

.tour-shot img {
  display: block;
  width: 100%;
  aspect-ratio: 16 / 9.2;
  object-fit: cover;
  object-position: top left;
}

.tour-caption {
  margin: 16px 0 0;
  text-align: center;
  color: var(--vp-c-text-2);
  font-size: 14px;
}

/* More */
.more {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  border: 1px solid var(--c-line);
  border-radius: 4px;
  overflow: hidden;
  gap: 1px;
  background: var(--c-line);
}

.more span {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 16px 18px;
  background: var(--vp-c-bg);
  font-size: 14px;
}

.more i {
  font-style: normal;
  color: var(--c-ok);
  font-weight: 700;
}

/* Comparison */
.table-scroll {
  overflow-x: auto;
  border: 1px solid var(--c-line);
  border-radius: 4px;
}

.cmp {
  width: 100%;
  min-width: 640px;
  border-collapse: collapse;
  font-size: 14.5px;
}

.cmp th,
.cmp td {
  padding: 14px 18px;
  border-bottom: 1px solid var(--c-line);
  text-align: center;
}

.cmp th:first-child,
.cmp td:first-child {
  text-align: left;
}

.cmp th {
  font-size: 13px;
  font-weight: 600;
  color: var(--vp-c-text-2);
  background: var(--c-panel-2);
}

.cmp tr:last-child td {
  border-bottom: 0;
}

.cmp .us {
  background: var(--c-accent-soft);
  color: var(--c-accent-text);
}

.yes { color: var(--c-ok); font-weight: 700; }
.no { color: var(--c-bad); opacity: 0.75; }
.partial { color: var(--vp-c-text-2); font-size: 13px; }
.partial.strong { color: var(--c-accent-text); font-weight: 600; }

/* Split */
.split {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0;
}

.split > div {
  padding: 8px 40px 8px 0;
}

.split > div + div {
  padding: 8px 0 8px 40px;
  border-left: 1px solid var(--c-line);
}

.sec-list,
.steps {
  list-style: none;
  margin: 26px 0 24px;
  padding: 0;
  display: grid;
  gap: 16px;
}

.sec-list li,
.steps li {
  display: grid;
  gap: 3px;
  padding-left: 32px;
  position: relative;
}

.sec-list li::before,
.steps li::before {
  position: absolute;
  left: 0;
  top: 1px;
  width: 20px;
  height: 20px;
  border-radius: 4px;
  display: grid;
  place-items: center;
  font-size: 11px;
  font-weight: 700;
}

.sec-list li::before {
  content: '✓';
  color: var(--c-accent-text);
  background: var(--c-accent-soft);
}

.steps {
  counter-reset: step;
}

.steps li {
  counter-increment: step;
}

.steps li::before {
  content: counter(step);
  color: var(--c-btn-text);
  background: var(--c-btn-bg);
}

.sec-list strong,
.steps strong {
  font-size: 14.5px;
}

.sec-list span,
.steps span {
  color: var(--vp-c-text-2);
  font-size: 14px;
  line-height: 1.55;
}

.steps code {
  font-size: 12.5px;
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--c-panel-3);
}

/* FAQ */
.head p a {
  color: var(--c-accent-text);
}

.faq {
  border: 1px solid var(--c-line);
  border-radius: 4px;
  background: var(--c-panel);
}

.faq details {
  border-bottom: 1px solid var(--c-line);
}

.faq details:last-child {
  border-bottom: 0;
}

.faq summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 22px;
  font-size: 15.5px;
  font-weight: 600;
  letter-spacing: -0.01em;
  cursor: pointer;
  list-style: none;
}

.faq summary::-webkit-details-marker {
  display: none;
}

.faq summary:hover {
  background: var(--c-panel-2);
}

.plus {
  position: relative;
  flex: none;
  width: 12px;
  height: 12px;
}

.plus::before,
.plus::after {
  content: '';
  position: absolute;
  left: 0;
  top: 5px;
  width: 12px;
  height: 2px;
  border-radius: 1px;
  background: var(--vp-c-text-3);
  transition: transform 0.2s;
}

.plus::after {
  transform: rotate(90deg);
}

.faq details[open] .plus::after {
  transform: rotate(0deg);
}

.faq details p {
  margin: 0;
  padding: 0 22px 20px;
  max-width: 760px;
  color: var(--vp-c-text-2);
  font-size: 14.5px;
  line-height: 1.7;
}

.faq details p a {
  margin-left: 4px;
  color: var(--c-accent-text);
  font-weight: 600;
  text-decoration: none;
  white-space: nowrap;
}

.faq details p a:hover {
  text-decoration: underline;
}

/* CTA */
.final {
  padding: 72px 24px;
  border: 1px solid var(--c-line);
  border-radius: 4px;
  text-align: center;
}

.final img {
  margin: 0 auto 18px;
  border-radius: 10px;
}

.final p {
  margin: 12px auto 0;
  color: var(--vp-c-text-2);
}

.final .actions {
  justify-content: center;
}

/* Responsive */
@media (max-width: 1060px) {
  .hero-grid {
    grid-template-columns: 1fr;
    gap: 72px;
  }

  .hero-mock {
    margin: 0 auto;
    width: 100%;
  }

  .block-body {
    grid-template-columns: 1fr;
  }

  .items {
    border-right: 0;
    border-bottom: 1px solid var(--c-line);
  }
}

@media (max-width: 860px) {
  .container {
    padding: 0 20px;
  }

  .rails > .container {
    border-inline: 0;
  }

  .block-head,
  .agents-head {
    flex-direction: column;
    align-items: flex-start;
  }

  .agents-points,
  .more {
    grid-template-columns: 1fr;
  }

  .split {
    grid-template-columns: 1fr;
  }

  .split > div,
  .split > div + div {
    padding: 0;
    border: 0;
  }

  .split > div + div {
    margin-top: 48px;
  }

  .stage,
  .tour-stage {
    padding: 20px;
  }

  .tour-stage {
    padding-bottom: 0;
  }
}

@media (max-width: 640px) {
  .hero-grid {
    padding-top: 48px;
    padding-bottom: 64px;
  }

  .install code {
    font-size: 11.5px;
  }
}
</style>
