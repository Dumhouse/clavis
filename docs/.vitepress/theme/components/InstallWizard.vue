<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

/*
 * Builds a docker-compose.yaml for the user's situation. Everything runs in the
 * browser: the setup token is generated locally with crypto.getRandomValues and
 * never leaves this page.
 */

type Scenario = 'local' | 'lan' | 'public' | 'proxy'
type TLSMode = 'off' | 'self-signed' | 'acme-dns' | 'file' | 'acme'

const scenarios: { id: Scenario; title: string; text: string }[] = [
  { id: 'local', title: 'Try it on this computer', text: 'Open it at localhost. No certificate needed.' },
  { id: 'lan', title: 'Home lab or office network', text: 'A server you reach by IP or a local name, like 192.168.1.20.' },
  { id: 'public', title: 'Public server with a domain', text: 'clavis.example.com points at this server and ports 80 and 443 are open.' },
  { id: 'proxy', title: 'Behind my reverse proxy', text: 'Caddy, nginx, Traefik or Cloudflare already handles HTTPS.' },
]

const tlsLabels: Record<TLSMode, string> = {
  off: 'No HTTPS (HTTP only)',
  'self-signed': 'Self-signed certificate',
  file: 'My own certificate files',
  acme: "Let's Encrypt (automatic)",
  'acme-dns': "Let's Encrypt via DNS (I own a domain)",
}

const scenario = ref<Scenario>('lan')
const host = ref('192.168.1.20')
const tls = ref<TLSMode>('self-signed')
const httpPort = ref(80)
const httpsPort = ref(443)
const ownerEmail = ref('')
const acmeEmail = ref('')
const dnsProvider = ref<'cloudflare' | 'duckdns'>('cloudflare')
const presetToken = ref(true)
const token = ref('')
const registry = ref<'dumhouse/clavis' | 'ghcr.io/dumhouse/clavis'>('dumhouse/clavis')
const tag = ref('latest')
const copied = ref('')

const tlsChoices = computed<TLSMode[]>(() => {
  switch (scenario.value) {
    case 'local':
      return ['off']
    case 'lan':
      return ['self-signed', 'acme-dns', 'file', 'off']
    case 'public':
      return ['acme', 'file']
    case 'proxy':
      return ['off']
  }
})

watch(tls, (next) => {
  if (next === 'acme-dns' && (/^\d/.test(host.value) || !host.value.includes('.'))) {
    host.value = dnsProvider.value === 'duckdns' ? 'myhome.duckdns.org' : 'clavis.home.example.com'
  }
})

watch(dnsProvider, (next) => {
  if (tls.value !== 'acme-dns') return
  if (next === 'duckdns' && !host.value.endsWith('.duckdns.org')) host.value = 'myhome.duckdns.org'
  if (next === 'cloudflare' && host.value.endsWith('.duckdns.org')) host.value = 'clavis.home.example.com'
})

watch(scenario, (next) => {
  tls.value = tlsChoices.value[0]
  if (next === 'local') host.value = 'localhost'
  if (next === 'lan' && (host.value === 'localhost' || host.value.includes('example'))) host.value = '192.168.1.20'
  if ((next === 'public' || next === 'proxy') && !host.value.includes('.') ) host.value = 'clavis.example.com'
  if ((next === 'public' || next === 'proxy') && /^\d/.test(host.value)) host.value = 'clavis.example.com'
})

function newToken() {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  token.value = btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

onMounted(() => {
  newToken()
  // The setup page links here with ?scenario=lan&host=<its host>.
  const params = new URLSearchParams(window.location.search)
  const s = params.get('scenario')
  if (s === 'local' || s === 'lan' || s === 'public' || s === 'proxy') scenario.value = s
  const h = params.get('host')
  if (h) setTimeout(() => (host.value = h), 0) // after the scenario watcher
})

const usesTLS = computed(() => tls.value !== 'off')
const isACME = computed(() => tls.value === 'acme' || tls.value === 'acme-dns')
const cleanHost = computed(() => host.value.trim().replace(/^https?:\/\//, '').replace(/\/.*$/, '') || 'localhost')

const baseURL = computed(() => {
  if (scenario.value === 'proxy') return `https://${cleanHost.value}`
  if (usesTLS.value) return `https://${cleanHost.value}${httpsPort.value === 443 ? '' : `:${httpsPort.value}`}`
  return `http://${cleanHost.value}${httpPort.value === 80 ? '' : `:${httpPort.value}`}`
})

const image = computed(() => `${registry.value}:${tag.value.trim() || 'latest'}`)

const env = computed(() => {
  const out: [string, string][] = [['CLAVIS_SERVER_BASE_URL', baseURL.value]]
  if (usesTLS.value) out.push(['CLAVIS_TLS', isACME.value ? 'acme' : tls.value])
  if (tls.value === 'acme-dns') {
    out.push(['CLAVIS_ACME_DNS', dnsProvider.value])
    out.push(['CLAVIS_ACME_DNS_TOKEN', `your-${dnsProvider.value}-token`])
  }
  if (isACME.value && acmeEmail.value.trim()) out.push(['CLAVIS_ACME_EMAIL', acmeEmail.value.trim()])
  if (tls.value === 'file') {
    out.push(['CLAVIS_TLS_CERT_FILE', '/certs/fullchain.pem'])
    out.push(['CLAVIS_TLS_KEY_FILE', '/certs/privkey.pem'])
  }
  if (scenario.value === 'proxy') out.push(['CLAVIS_TRUSTED_PROXIES', '172.16.0.0/12'])
  if (ownerEmail.value.trim()) out.push(['CLAVIS_SETUP_OWNER_EMAIL', ownerEmail.value.trim()])
  if (presetToken.value) out.push(['CLAVIS_SETUP_TOKEN', token.value])
  return out
})

const ports = computed(() => {
  if (scenario.value === 'proxy') return ['127.0.0.1:8080:8080']
  const out = [`${httpPort.value}:8080`]
  if (usesTLS.value) out.push(`${httpsPort.value}:8443`)
  return out
})

const compose = computed(() => {
  const lines = [
    'services:',
    '  clavis:',
    `    image: ${image.value}`,
    '    container_name: clavis',
    '    restart: unless-stopped',
    '    ports:',
    ...ports.value.map((p) => `      - "${p}"`),
    '    environment:',
    ...env.value.map(([k, v]) => `      ${k}: "${v}"`),
    '    volumes:',
    '      - clavis-data:/data',
  ]
  if (tls.value === 'file') lines.push('      - ./certs:/certs:ro')
  lines.push('', 'volumes:', '  clavis-data:', '')
  return lines.join('\n')
})

const dockerRun = computed(() => {
  const parts = ['docker run -d --name clavis --restart unless-stopped']
  for (const p of ports.value) parts.push(`-p ${p}`)
  parts.push('-v clavis-data:/data')
  if (tls.value === 'file') parts.push('-v "$PWD/certs:/certs:ro"')
  for (const [k, v] of env.value) parts.push(`-e ${k}="${v}"`)
  parts.push(image.value)
  return parts.join(' ')
})

const setupLink = computed(() =>
  presetToken.value ? `${baseURL.value}/setup#token=${token.value}` : `${baseURL.value}/setup`,
)

const caddyfile = computed(() => `${cleanHost.value} {\n  reverse_proxy 127.0.0.1:8080\n}\n`)

async function copy(id: string, text: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = id
    setTimeout(() => (copied.value = ''), 1500)
  } catch {
    copied.value = ''
  }
}

function download() {
  const blob = new Blob([compose.value], { type: 'text/yaml' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'compose.yaml'
  a.click()
  URL.revokeObjectURL(a.href)
}
</script>

<template>
  <div class="wiz">
    <section>
      <h3>1. Where will Clavis run?</h3>
      <div class="cards">
        <label v-for="s in scenarios" :key="s.id" class="card" :class="{ on: scenario === s.id }">
          <input v-model="scenario" type="radio" name="scenario" :value="s.id" />
          <strong>{{ s.title }}</strong>
          <span>{{ s.text }}</span>
        </label>
      </div>
    </section>

    <section v-if="scenario !== 'local'">
      <h3>2. Address</h3>
      <label class="field">
        <span>{{ tls === 'acme-dns' ? 'Name in your domain' : scenario === 'lan' ? 'IP address or local name' : 'Domain name' }}</span>
        <input v-model="host" type="text" spellcheck="false" />
      </label>
    </section>

    <section>
      <h3>{{ scenario === 'local' ? '2' : '3' }}. HTTPS</h3>
      <div class="opts">
        <label v-for="m in tlsChoices" :key="m" class="opt">
          <input v-model="tls" type="radio" name="tls" :value="m" />
          {{ tlsLabels[m] }}<span v-if="m === tlsChoices[0] && tlsChoices.length > 1" class="rec">recommended</span>
        </label>
      </div>
      <p v-if="scenario === 'local'" class="hint">Browsers treat localhost as secure, so no certificate is needed.</p>
      <p v-else-if="scenario === 'proxy'" class="hint">
        Your proxy handles HTTPS. Clavis listens on 127.0.0.1:8080 and your proxy forwards
        <code>{{ cleanHost }}</code> to it. Passkeys and secure cookies work through the proxy.
      </p>
      <p v-else-if="tls === 'self-signed'" class="hint">
        Clavis creates a certificate for <code>{{ cleanHost }}</code>. Browsers warn once; trust the certificate on your
        devices to remove the warning. The CLI shows the fingerprint and asks once per computer. Passkeys need a trusted
        certificate and a name, not an IP.
      </p>
      <p v-else-if="tls === 'acme'" class="hint">
        Clavis gets and renews a free certificate. <code>{{ cleanHost }}</code> must resolve to this server and ports 80 and
        443 must be reachable from the internet.
      </p>
      <p v-else-if="tls === 'acme-dns'" class="hint">
        A real certificate for a server that's only on your network: Let's Encrypt checks a DNS record instead of
        connecting to it, so no port is opened. Every browser and CLI trusts it with no setup. Use a name in your
        domain, like <code>clavis.home.example.com</code>, that resolves to the server on your LAN, and replace the token
        placeholder below. <a href="./https#let-s-encrypt-on-a-private-network">Details</a>
      </p>
      <p v-else-if="tls === 'file'" class="hint">
        Put <code>fullchain.pem</code> and <code>privkey.pem</code> in <code>./certs</code> next to the compose file. Clavis
        picks up renewed files without a restart.
      </p>
      <p v-else class="hint warn">
        Without HTTPS your password and secrets cross the network unencrypted and passkeys are off. Fine for a quick test
        on a trusted network only.
      </p>
      <div v-if="tls === 'acme-dns'" class="opts">
        <label class="opt"><input v-model="dnsProvider" type="radio" name="dns" value="cloudflare" /> Cloudflare</label>
        <label class="opt"><input v-model="dnsProvider" type="radio" name="dns" value="duckdns" /> DuckDNS</label>
      </div>
      <label v-if="isACME" class="field">
        <span>Email for Let's Encrypt expiry notices (optional)</span>
        <input v-model="acmeEmail" type="email" placeholder="you@example.com" />
      </label>
    </section>

    <section v-if="scenario !== 'proxy'">
      <h3>Ports on the host</h3>
      <div class="row">
        <label class="field small">
          <span>HTTP</span>
          <input v-model.number="httpPort" type="number" min="1" max="65535" />
        </label>
        <label v-if="usesTLS" class="field small">
          <span>HTTPS</span>
          <input v-model.number="httpsPort" type="number" min="1" max="65535" />
        </label>
      </div>
      <p v-if="usesTLS" class="hint">The HTTP port redirects to HTTPS{{ tls === 'acme' ? ' and answers Let\'s Encrypt checks (keep it at 80)' : '' }}.</p>
    </section>

    <section>
      <h3>First sign-in</h3>
      <label class="field">
        <span>Owner email (optional, only this address can claim the server)</span>
        <input v-model="ownerEmail" type="email" placeholder="you@example.com" />
      </label>
      <label class="check">
        <input v-model="presetToken" type="checkbox" />
        Put a setup token in the file, so you get a ready-to-click setup link (no token hunting)
      </label>
      <p v-if="presetToken" class="hint">
        Generated in your browser and never sent anywhere.
        <button type="button" class="link" @click="newToken">Generate another</button>
      </p>
    </section>

    <section>
      <h3>Image</h3>
      <div class="row">
        <label class="field">
          <span>Registry</span>
          <select v-model="registry">
            <option value="dumhouse/clavis">Docker Hub (dumhouse/clavis)</option>
            <option value="ghcr.io/dumhouse/clavis">GitHub (ghcr.io/dumhouse/clavis)</option>
          </select>
        </label>
        <label class="field small">
          <span>Version</span>
          <input v-model="tag" type="text" placeholder="latest" />
        </label>
      </div>
    </section>

    <section class="out">
      <div class="out-head">
        <h3>compose.yaml</h3>
        <span>
          <button type="button" @click="copy('compose', compose)">{{ copied === 'compose' ? 'Copied' : 'Copy' }}</button>
          <button type="button" @click="download">Download</button>
        </span>
      </div>
      <pre><code>{{ compose }}</code></pre>

      <h3>Start it</h3>
      <pre><code>docker compose up -d</code></pre>

      <div class="out-head">
        <h3>Or as one docker command (no compose file)</h3>
        <button type="button" @click="copy('run', dockerRun)">{{ copied === 'run' ? 'Copied' : 'Copy' }}</button>
      </div>
      <pre class="wrap"><code>{{ dockerRun }}</code></pre>

      <template v-if="scenario === 'proxy'">
        <h3>Caddyfile (if you use Caddy)</h3>
        <pre><code>{{ caddyfile }}</code></pre>
      </template>

      <div class="out-head">
        <h3>Then open</h3>
        <button type="button" @click="copy('link', setupLink)">{{ copied === 'link' ? 'Copied' : 'Copy' }}</button>
      </div>
      <pre><code>{{ setupLink }}</code></pre>
      <p v-if="presetToken" class="hint">
        The link carries the setup token, so the wizard skips that step. Keep it private until you have created the owner
        account; after that the token stops working.
      </p>
      <p v-else class="hint">
        The server log prints this link with the token included:
        <code>docker logs clavis 2>&1 | grep setup_url</code>. Or print the token with
        <code>docker exec clavis clavis setup-token</code>.
      </p>

    </section>
  </div>
</template>

<style scoped>
.wiz { display: grid; gap: 1.25rem; margin: 1.5rem 0; }
.wiz h3 { margin: 0 0 0.5rem; font-size: 1rem; border: 0; padding: 0; }
.cards { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr)); }
.card { display: grid; gap: 0.2rem; padding: 0.75rem; border: 1px solid var(--vp-c-divider); border-radius: 8px; cursor: pointer; }
.card.on { border-color: var(--vp-c-brand-1); background: var(--vp-c-brand-soft); }
.card input { display: none; }
.card span { font-size: 0.85rem; color: var(--vp-c-text-2); line-height: 1.4; }
.opts { display: grid; gap: 0.35rem; }
.opt, .check { display: flex; align-items: center; gap: 0.5rem; font-size: 0.95rem; }
.rec { margin-left: 0.4rem; font-size: 0.75rem; color: var(--vp-c-brand-1); }
.field { display: grid; gap: 0.25rem; font-size: 0.85rem; color: var(--vp-c-text-2); flex: 1; }
.field.small { flex: 0 0 8rem; }
.field input, .field select { padding: 0.4rem 0.6rem; border: 1px solid var(--vp-c-border); border-radius: 6px; background: var(--vp-c-bg); color: var(--vp-c-text-1); font-size: 0.95rem; }
.row { display: flex; gap: 0.75rem; flex-wrap: wrap; }
.hint { margin: 0.4rem 0 0; font-size: 0.85rem; color: var(--vp-c-text-2); }
.hint.warn { color: var(--vp-c-warning-1); }
.link { color: var(--vp-c-brand-1); text-decoration: underline; font-size: 0.85rem; }
.out { border-top: 1px solid var(--vp-c-divider); padding-top: 1rem; }
.out-head { display: flex; justify-content: space-between; align-items: center; margin-top: 0.75rem; }
.out-head button, .out-head span button { margin-left: 0.4rem; padding: 0.2rem 0.6rem; border: 1px solid var(--vp-c-border); border-radius: 6px; font-size: 0.8rem; }
.out pre { margin: 0.4rem 0 0; padding: 0.75rem 1rem; background: var(--vp-code-block-bg); border-radius: 8px; overflow-x: auto; font-size: 0.85rem; line-height: 1.5; }
.out pre.wrap { white-space: pre-wrap; word-break: break-all; }
</style>
