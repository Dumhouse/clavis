<script setup lang="ts">
import { REPO_URL, formatCount, useGitHub } from '../github'

const gh = useGitHub()
</script>

<template>
  <a v-if="gh.stars !== null" class="nav-stars" :href="REPO_URL" :aria-label="`Star Clavis on GitHub (${gh.stars} stars)`">
    <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true"><path fill="currentColor" d="m12 2.5 2.9 6.1 6.6.8-4.9 4.6 1.3 6.6L12 17.3l-5.9 3.3 1.3-6.6-4.9-4.6 6.6-.8L12 2.5Z" /></svg>
    {{ formatCount(gh.stars) }}
  </a>
</template>

<style scoped>
.nav-stars {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  margin-left: 12px;
  padding: 0 10px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 6px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--vp-c-text-1);
  text-decoration: none;
  white-space: nowrap;
}

.nav-stars svg {
  color: #f59e0b;
}

.nav-stars:hover {
  border-color: var(--c-line-strong);
}

@media (max-width: 767px) {
  .nav-stars {
    display: none;
  }
}
</style>
