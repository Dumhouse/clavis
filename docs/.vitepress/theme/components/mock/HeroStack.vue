<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

/*
 * Layered panels: the screens behind the secrets grid peek out above it.
 * A short loop ticks keys in the promote card, promotes them, and updates
 * the grid. Reduced-motion users see the finished state without the loop.
 */
const step = ref(0)
const promoted = computed(() => step.value >= 5)
const busy = computed(() => step.value === 4)

const rows = computed(() => [
  { key: 'DATABASE_URL', dev: 'set', stg: 'set', prod: 'set' },
  { key: 'STRIPE_SECRET_KEY', dev: 'set', stg: 'set', prod: 'set' },
  { key: 'REDIS_URL', dev: 'set', stg: promoted.value ? 'new' : 'inherit', prod: 'missing' },
  { key: 'LOG_LEVEL', dev: 'set', stg: 'set', prod: 'set' },
  { key: 'FEATURE_FLAGS', dev: 'set', stg: promoted.value ? 'new' : 'changed', prod: 'set' },
])

const root = ref<HTMLElement | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined
let observer: IntersectionObserver | undefined
let visible = false
const delays = [900, 700, 700, 700, 900, 2600]

function tick() {
  if (!visible) return
  timer = setTimeout(() => {
    step.value = (step.value + 1) % delays.length
    tick()
  }, delays[step.value])
}

onMounted(() => {
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    step.value = 2
    return
  }
  observer = new IntersectionObserver(([entry]) => {
    visible = entry.isIntersecting
    clearTimeout(timer)
    if (visible) tick()
  })
  if (root.value) observer.observe(root.value)
  // After client-side navigation the first observer callback can report the
  // element as hidden while the old page is still laid out; re-check once.
  setTimeout(() => {
    const r = root.value?.getBoundingClientRect()
    if (!visible && r && r.bottom > 0 && r.top < window.innerHeight) {
      visible = true
      clearTimeout(timer)
      tick()
    }
  }, 900)
})

onBeforeUnmount(() => {
  clearTimeout(timer)
  observer?.disconnect()
})
</script>

<template>
  <div ref="root" class="stack" aria-label="Clavis product screens">
    <div class="m-win layer l3">
      <div class="m-head"><span class="sq" />Audit log<div class="m-tabs"><span class="on">All</span><span>Reveals</span><span>Agents</span></div></div>
    </div>
    <div class="m-win layer l2">
      <div class="m-head"><span class="sq" />Dynamic leases<div class="m-tabs"><span class="on">Active</span><span>Expired</span></div></div>
    </div>
    <div class="m-win layer l1">
      <div class="m-head"><span class="sq" />Agents &amp; machines<div class="m-tabs"><span class="on">All</span><span>Agents</span><span>CI</span></div></div>
    </div>

    <div class="m-win front">
      <div class="m-head">
        <span class="sq" />Secrets <span class="sub">payments-api</span>
        <div class="m-tabs"><span class="on">Grid</span><span>Schema</span><span>History</span></div>
      </div>
      <table class="m-table">
        <thead>
          <tr>
            <th>Key</th>
            <th class="c"><span class="m-env dev">dev</span></th>
            <th class="c"><span class="m-env stg">staging</span></th>
            <th class="c"><span class="m-env prod">prod</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.key">
            <td>
              <span class="m-key">
                <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true"><path d="M15 7a4 4 0 1 1-3.87 5H8v2H6v2H3v-3l5.13-5.13A4 4 0 0 1 15 7Z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" /></svg>
                {{ r.key }}
              </span>
            </td>
            <td v-for="env in ['dev', 'stg', 'prod']" :key="env" class="c">
              <span v-if="(r as any)[env] === 'set'" class="m-dots">••••••</span>
              <span v-else-if="(r as any)[env] === 'new'" class="m-dots fresh">••••••</span>
              <span v-else-if="(r as any)[env] === 'inherit'" class="m-chip">inherits dev</span>
              <span v-else-if="(r as any)[env] === 'changed'" class="m-chip amber">changed</span>
              <span v-else class="m-chip red">missing</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="m-win pop">
      <div class="m-head">Promote dev → staging</div>
      <ul>
        <li><span class="m-check" :class="{ on: step >= 1 }">{{ step >= 1 ? '✓' : '' }}</span><span class="m-mono">FEATURE_FLAGS</span><span class="m-chip amber">changed</span></li>
        <li><span class="m-check" :class="{ on: step >= 2 }">{{ step >= 2 ? '✓' : '' }}</span><span class="m-mono">REDIS_URL</span><span class="m-chip green">added</span></li>
        <li><span class="m-check" /><span class="m-mono">LOG_LEVEL</span><span class="m-chip">unchanged</span></li>
      </ul>
      <div class="foot">
        <span><span class="m-check on">✓</span> Snapshot target first</span>
        <span class="m-btn primary go" :class="{ press: step === 3, busy }">
          {{ promoted ? '✓ Promoted' : busy ? 'Promoting…' : step >= 2 ? 'Promote 2' : step === 1 ? 'Promote 1' : 'Promote' }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.stack {
  position: relative;
  width: 100%;
  height: 470px;
  max-width: 600px;
  margin-left: auto;
}

.layer,
.front,
.pop {
  animation: rise 0.7s cubic-bezier(0.2, 0.7, 0.2, 1) both;
}

.l3 { animation-delay: 0.05s; }
.l2 { animation-delay: 0.15s; }
.l1 { animation-delay: 0.25s; }
.front { animation-delay: 0.35s; }
.pop { animation-delay: 0.6s; }

@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(14px);
  }
}

.m-check {
  transition: background 0.2s, border-color 0.2s;
}

.go {
  min-width: 96px;
  justify-content: center;
  transition: transform 0.12s, opacity 0.2s;
}

.go.press {
  transform: scale(0.94);
}

.go.busy {
  opacity: 0.7;
}

.fresh {
  animation: flash 1.6s ease-out;
}

@keyframes flash {
  0% {
    background: var(--c-accent);
    color: #fff;
  }
}

@media (prefers-reduced-motion: reduce) {
  .layer,
  .front,
  .pop,
  .fresh {
    animation: none;
  }
}

.layer {
  position: absolute;
  height: 120px;
  right: 0;
}

.l3 {
  top: 0;
  left: 96px;
  right: -24px;
  opacity: 0.6;
}

.l2 {
  top: 40px;
  left: 64px;
  right: -12px;
  opacity: 0.8;
}

.l1 {
  top: 80px;
  left: 32px;
  right: 0;
}

.front {
  position: absolute;
  top: 120px;
  left: 0;
  right: 12px;
  box-shadow: var(--c-shadow-lg);
}

.pop {
  position: absolute;
  left: -36px;
  bottom: -30px;
  width: 290px;
  box-shadow: var(--c-shadow-lg);
}

.pop .m-head {
  min-height: 36px;
}

.pop ul {
  list-style: none;
  margin: 0;
  padding: 8px 12px;
  display: grid;
  gap: 8px;
}

.pop li {
  display: flex;
  align-items: center;
  gap: 9px;
}

.pop li .m-chip {
  margin-left: auto;
}

.foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px;
  border-top: 1px solid var(--c-line);
  font-size: 12px;
  color: var(--vp-c-text-2);
}

.foot > span:first-child {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}

@media (max-width: 640px) {
  .stack {
    height: auto;
  }

  .layer,
  .pop {
    display: none;
  }

  .front {
    position: static;
    overflow-x: auto;
  }
}
</style>
