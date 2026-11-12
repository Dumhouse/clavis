<script setup lang="ts">
/* A JIT access request awaiting approval, above short-lived dynamic leases. */
const leases = [
  { name: 'pg-readonly · v-ci-7f3a', ttl: '42m left', pct: 70 },
  { name: 'aws-sts · deploy-bot', ttl: '12m left', pct: 20 },
  { name: 'pg-migrate · v-ada-19c2', ttl: 'revoked on exit', pct: 0 },
]
</script>

<template>
  <div class="col">
    <div class="m-win req">
      <div class="m-head"><span class="sq warn" />Access request <span class="m-chip amber" style="margin-left: auto">pending</span></div>
      <div class="body">
        <div class="who">
          <span class="avatar">AL</span>
          <div>
            <strong>Ada Lovelace</strong> requests <strong>Admin</strong> on
            <span class="m-env prod">production</span> for <strong>1 hour</strong>
            <p class="reason">“Incident #412: rotate the leaked webhook key.”</p>
          </div>
        </div>
        <div class="actions">
          <span class="m-btn">Deny</span>
          <span class="m-btn primary">Approve for 1h</span>
        </div>
      </div>
    </div>
    <div class="m-win leases">
      <div class="m-head"><span class="sq" />Dynamic leases<div class="m-tabs"><span class="on">Active</span><span>Expired</span></div></div>
      <ul>
        <li v-for="l in leases" :key="l.name">
          <span class="m-mono">{{ l.name }}</span>
          <span class="bar"><i :style="{ width: l.pct + '%' }" /></span>
          <span class="m-muted ttl">{{ l.ttl }}</span>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.col {
  display: grid;
  gap: 16px;
  max-width: 520px;
  margin: 0 auto;
}

.sq.warn {
  background: #f59e0b !important;
}

.body {
  padding: 14px 16px;
}

.who {
  display: flex;
  gap: 12px;
  line-height: 1.6;
}

.avatar {
  flex: none;
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: var(--c-accent-soft);
  color: var(--c-accent-text);
  font-size: 11px;
  font-weight: 700;
}

.reason {
  margin: 6px 0 0;
  color: var(--vp-c-text-2);
  font-style: italic;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}

.leases {
  margin-left: 40px;
}

.leases ul {
  list-style: none;
  margin: 0;
  padding: 6px 0;
}

.leases li {
  display: grid;
  grid-template-columns: 1fr 80px 100px;
  align-items: center;
  gap: 12px;
  padding: 8px 14px;
}

.bar {
  height: 5px;
  border-radius: 9px;
  background: var(--c-panel-3);
  overflow: hidden;
}

.bar i {
  display: block;
  height: 100%;
  background: var(--c-accent);
}

.ttl {
  font-size: 11.5px;
  text-align: right;
}

@media (max-width: 640px) {
  .leases {
    margin-left: 0;
  }

  .leases li {
    grid-template-columns: 1fr 70px;
  }

  .bar {
    display: none;
  }
}
</style>
