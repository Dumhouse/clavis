<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter, withBase } from 'vitepress'

import reference from '../../cli-reference.json'

/*
 * Filter box on the CLI reference overview. Matches command paths, summaries,
 * descriptions and flag names ("--watch" finds `clavis run`).
 */

type Command = (typeof reference.commands)[number]

const query = ref('')
const active = ref(0)
const input = ref<HTMLInputElement | null>(null)
const router = useRouter()

function score(command: Command, needle: string): number {
  const path = command.path.toLowerCase()
  if (path === needle) return 100
  if (path.startsWith(needle)) return 80
  if (path.includes(needle)) return 60
  const flag = needle.replace(/^-+/, '')
  if (needle.startsWith('-') && command.flags.some((f) => f.name.startsWith(flag))) return 50
  if (command.summary.toLowerCase().includes(needle)) return 40
  if (command.description.toLowerCase().includes(needle)) return 20
  if (command.examples.some((e) => e.run.toLowerCase().includes(needle))) return 10
  return 0
}

const results = computed(() => {
  const needle = query.value.trim().toLowerCase().replace(/^clavis\s+/, '')
  if (!needle) return []
  return reference.commands
    .map((command) => ({ command, score: score(command, needle) }))
    .filter((entry) => entry.score > 0)
    .sort((a, b) => b.score - a.score || a.command.path.localeCompare(b.command.path))
    .slice(0, 10)
    .map((entry) => entry.command)
})

function open(command: Command | undefined) {
  if (command) void router.go(withBase(`/reference/${command.slug}`))
}

function onKey(event: KeyboardEvent) {
  if (event.key === 'ArrowDown') {
    active.value = Math.min(active.value + 1, results.value.length - 1)
    event.preventDefault()
  } else if (event.key === 'ArrowUp') {
    active.value = Math.max(active.value - 1, 0)
    event.preventDefault()
  } else if (event.key === 'Enter') {
    open(results.value[active.value])
  } else if (event.key === 'Escape') {
    query.value = ''
  }
}

function onSlash(event: KeyboardEvent) {
  const target = event.target as HTMLElement | null
  if (event.key !== '/' || target?.closest('input, textarea, [contenteditable]')) return
  event.preventDefault()
  input.value?.focus()
}

onMounted(() => document.addEventListener('keydown', onSlash))
onBeforeUnmount(() => document.removeEventListener('keydown', onSlash))
</script>

<template>
  <div class="cli-search">
    <label class="cli-search-box">
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="11" cy="11" r="7" fill="none" stroke="currentColor" stroke-width="2" />
        <path d="m20 20-3.5-3.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
      </svg>
      <input
        ref="input"
        v-model="query"
        type="search"
        placeholder="Find a command or flag, e.g. run, reset-password, --watch"
        aria-label="Find a command"
        autocomplete="off"
        spellcheck="false"
        @input="active = 0"
        @keydown="onKey"
      />
      <kbd aria-hidden="true">/</kbd>
    </label>
    <ul v-if="query.trim()" class="cli-search-results" role="listbox">
      <li v-for="(command, index) in results" :key="command.path" role="option" :aria-selected="index === active">
        <a
          :href="withBase(`/reference/${command.slug}`)"
          :class="{ active: index === active }"
          @mouseenter="active = index"
        >
          <code>clavis {{ command.path }}</code>
          <span>{{ command.summary }}</span>
        </a>
      </li>
      <li v-if="results.length === 0" class="cli-search-empty">No command matches “{{ query }}”.</li>
    </ul>
  </div>
</template>
