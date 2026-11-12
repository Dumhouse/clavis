<script setup lang="ts">
/* Zoomed-in schema coverage: which required keys are valid in each environment. */
const rows: [string, string, boolean, boolean, boolean][] = [
  ['DATABASE_URL', 'url', true, true, true],
  ['STRIPE_SECRET_KEY', 'string', true, true, true],
  ['SENTRY_DSN', 'url', true, true, false],
  ['LOG_LEVEL', 'enum', true, false, true],
  ['REDIS_URL', 'url', true, true, true],
  ['FEATURE_FLAGS', 'json', true, true, true],
  ['WEBHOOK_SIGNING_KEY', 'string', false, true, true],
]
</script>

<template>
  <div class="m-win wrap">
    <table class="m-table">
      <thead>
        <tr>
          <th>Key</th>
          <th>Type</th>
          <th class="c"><span class="m-env dev">Development</span></th>
          <th class="c"><span class="m-env stg">Staging</span></th>
          <th class="c"><span class="m-env prod">Production</span></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in rows" :key="r[0]">
          <td><span class="m-key">{{ r[0] }}</span></td>
          <td><span class="m-chip">{{ r[1] }}</span></td>
          <td v-for="i in [2, 3, 4]" :key="i" class="c">
            <span v-if="r[i]" class="m-ok" aria-label="valid">✓</span>
            <span v-else class="m-bad" aria-label="invalid">✕</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.wrap {
  overflow-x: auto;
}

.m-table td,
.m-table th {
  padding: 12px 18px;
}

.m-key {
  font-size: 13.5px;
}
</style>
