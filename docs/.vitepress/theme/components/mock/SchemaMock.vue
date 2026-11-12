<script setup lang="ts">
/* A committed schema file and the CI check that enforces it. */
</script>

<template>
  <div class="pair">
    <div class="m-win file">
      <div class="m-head"><span class="sq" />clavis.schema.yaml</div>
      <pre class="m-mono"><span class="k">LOG_LEVEL</span>:
  type: <span class="s">enum</span>
  values: [debug, info, warn, error]
  required: { production: <span class="s">true</span> }
<span class="k">SENTRY_DSN</span>:
  type: <span class="s">url</span>
  pattern: <span class="s">^https://.+@sentry\.io/\d+$</span>
  required: { staging: <span class="s">true</span>, production: <span class="s">true</span> }
<span class="k">PORT</span>:
  kind: <span class="s">variable</span>
  type: <span class="s">int</span></pre>
    </div>
    <div class="m-win check">
      <div class="m-head"><span class="sq bad" />clavis check --env production</div>
      <ul>
        <li><span class="m-ok">✓</span><span class="m-mono">DATABASE_URL</span><span class="m-muted">url</span></li>
        <li><span class="m-ok">✓</span><span class="m-mono">PORT</span><span class="m-muted">int</span></li>
        <li><span class="m-bad">✕</span><span class="m-mono">SENTRY_DSN</span><span class="m-chip red">missing</span></li>
        <li><span class="m-bad">✕</span><span class="m-mono">LOG_LEVEL</span><span class="m-chip red">"verbose" not allowed</span></li>
      </ul>
      <div class="exit m-mono">exit 6 · 2 errors · build failed</div>
    </div>
  </div>
</template>

<style scoped>
.pair {
  position: relative;
  padding-bottom: 70px;
}

.file {
  max-width: 470px;
}

.file pre {
  margin: 0;
  padding: 14px 16px;
  line-height: 1.75;
  color: var(--vp-c-text-2);
  overflow-x: auto;
}

.k {
  color: var(--vp-c-text-1);
  font-weight: 600;
}

.s {
  color: var(--c-accent-text);
}

.check {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 320px;
  box-shadow: var(--c-shadow-lg);
}

.sq.bad {
  background: var(--c-bad) !important;
}

.check ul {
  list-style: none;
  margin: 0;
  padding: 10px 14px;
  display: grid;
  gap: 8px;
}

.check li {
  display: flex;
  align-items: center;
  gap: 9px;
}

.check li > :last-child {
  margin-left: auto;
}

.exit {
  padding: 9px 14px;
  border-top: 1px solid var(--c-line);
  color: var(--c-bad);
  font-size: 11.5px;
}

@media (max-width: 640px) {
  .pair {
    padding-bottom: 0;
    display: grid;
    gap: 12px;
  }

  .check {
    position: static;
    width: auto;
  }
}
</style>
