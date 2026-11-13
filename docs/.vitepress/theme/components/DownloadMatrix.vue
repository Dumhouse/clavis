<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { REPO, REPO_URL } from '../github'

type Asset = { name: string; browser_download_url: string }

const tag = ref<string | null>(null)
const assets = ref<Asset[]>([])
const failed = ref(false)

onMounted(async () => {
  try {
    const res = await fetch(`https://api.github.com/repos/${REPO}/releases/latest`, {
      headers: { Accept: 'application/vnd.github+json' },
    })
    if (!res.ok) throw new Error(String(res.status))
    const data = await res.json()
    tag.value = data.tag_name
    assets.value = data.assets
  } catch {
    failed.value = true
  }
})

const rows = [
  { os: 'macOS', key: 'darwin', kinds: [['tar.gz', '.tar.gz']] },
  {
    os: 'Linux',
    key: 'linux',
    kinds: [
      ['tar.gz', '.tar.gz'],
      ['.deb', '.deb'],
      ['.rpm', '.rpm'],
      ['.apk', '.apk'],
    ],
  },
  { os: 'Windows', key: 'windows', kinds: [['zip', '.zip']] },
]
const arches = [
  ['amd64', 'x86-64 (amd64)'],
  ['arm64', 'ARM (arm64)'],
]

function find(os: string, arch: string, ext: string) {
  return assets.value.find((a) => a.name.endsWith(`_${os}_${arch}${ext}`))
}

const checksums = computed(() => assets.value.find((a) => a.name === 'checksums.txt'))
</script>

<template>
  <div class="dl">
    <p v-if="failed" class="note">
      Could not load the latest release. Open the
      <a :href="`${REPO_URL}/releases/latest`">releases page</a> instead.
    </p>
    <p v-else-if="!tag" class="note">Loading the latest release…</p>
    <template v-else>
      <p class="note">
        Latest release: <a :href="`${REPO_URL}/releases/tag/${tag}`"><strong>{{ tag }}</strong></a>
      </p>
      <table>
        <thead>
          <tr>
            <th></th>
            <th v-for="[, label] in arches" :key="label">{{ label }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.os">
            <th scope="row">{{ row.os }}</th>
            <td v-for="[arch] in arches" :key="arch">
              <template v-for="[label, ext] in row.kinds" :key="ext">
                <a v-if="find(row.key, arch, ext)" :href="find(row.key, arch, ext)!.browser_download_url" class="chip">{{ label }}</a>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
      <p class="note">
        Apple Silicon Macs and most ARM servers are arm64. Most Intel and AMD machines are amd64.
        <template v-if="checksums">
          Verify downloads with <a :href="checksums.browser_download_url">checksums.txt</a>.
        </template>
      </p>
    </template>
  </div>
</template>

<style scoped>
.dl {
  margin: 16px 0;
}

.note {
  color: var(--vp-c-text-2);
  font-size: 14px;
}

table {
  width: 100%;
}

.chip {
  display: inline-block;
  margin: 2px 6px 2px 0;
  padding: 3px 10px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 999px;
  font-size: 13px;
  font-weight: 500;
  text-decoration: none;
}

.chip:hover {
  border-color: var(--vp-c-brand-1);
}
</style>
