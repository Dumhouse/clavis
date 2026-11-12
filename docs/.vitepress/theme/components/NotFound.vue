<script setup lang="ts">
import { withBase } from 'vitepress'
</script>

<template>
  <div class="nf">
    <p class="code">404</p>
    <h1>This page doesn't exist</h1>
    <p class="lead">It may have moved. Try search, or start from one of these.</p>
    <div class="links">
      <a :href="withBase('/')">Home</a>
      <a :href="withBase('/quickstart')">Quickstart</a>
      <a :href="withBase('/commands')">CLI reference</a>
      <a :href="withBase('/agents')">AI agents</a>
    </div>
  </div>
</template>

<style scoped>
.nf {
  max-width: 520px;
  margin: 0 auto;
  padding: 96px 24px;
  text-align: center;
}

.code {
  margin: 0;
  font: 600 14px/1 var(--vp-font-family-mono);
  color: var(--c-accent-text, var(--vp-c-brand-1));
  letter-spacing: 0.1em;
}

h1 {
  margin: 12px 0 8px;
  font-size: 28px;
  letter-spacing: -0.02em;
}

.lead {
  margin: 0 0 24px;
  color: var(--vp-c-text-2);
}

.links {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: center;
}

.links a {
  padding: 7px 14px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  font-size: 14px;
  text-decoration: none;
  color: var(--vp-c-text-1);
}

.links a:hover {
  border-color: var(--vp-c-brand-1);
}
</style>
