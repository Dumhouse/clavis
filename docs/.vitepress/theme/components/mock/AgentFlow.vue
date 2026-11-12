<script setup lang="ts">
/* How an agent call travels: sandbox → MCP → allowlist → command with secrets. */
</script>

<template>
  <div class="flow-diagram">
    <div class="chat m-win">
      <div class="m-head"><span class="sq" />Coding agent <span class="sub">sandboxed</span></div>
      <div class="msg">
        <span class="tag">tool</span>
        <span class="m-mono">run_with_secrets("pnpm test")</span>
      </div>
      <div class="msg out">
        <span class="m-mono">✓ 214 passed · DATABASE_URL=postgres://app:******@db</span>
      </div>
      <div class="msg">
        <span class="tag">tool</span>
        <span class="m-mono">run_with_secrets("printenv")</span>
      </div>
      <div class="msg out bad">
        <span class="m-mono">✕ denied: not on allowlist (audited)</span>
      </div>
    </div>

    <div class="path">
      <div class="node"><span class="dot" />clavis mcp --agent</div>
      <div class="line"><span>no token in config</span></div>
      <div class="node hl"><span class="dot" />allowlist check</div>
      <div class="line"><span>command audited before decrypt</span></div>
      <div class="node"><span class="dot" />Clavis server</div>
      <div class="branches">
        <div class="node small">pnpm test <em>+ secrets in env</em></div>
        <div class="node small">output masked</div>
        <div class="node small">audit event</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.flow-diagram {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 40px;
  align-items: center;
}

.chat {
  background: #111115;
  border-color: #2c2c34;
  color: #f4f4f5;
}

.chat .m-head {
  background: #0d0d10;
  border-color: #24242b;
}

.chat .sub {
  color: #8b8b95;
}

.msg {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  border-bottom: 1px solid #1e1e24;
  color: #e4e4e7;
}

.msg:last-child {
  border-bottom: 0;
}

.msg.out {
  color: #6ee7b7;
  padding-left: 54px;
}

.msg.bad {
  color: #fca5a5;
}

.tag {
  padding: 1px 7px;
  border-radius: 5px;
  background: #1f1f26;
  color: #a1a1aa;
  font-size: 10.5px;
  font-weight: 600;
  text-transform: uppercase;
}

.path {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.node {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 14px;
  border: 1px solid #34343d;
  background: #111115;
  color: #f4f4f5;
  font-family: var(--vp-font-family-mono);
  font-size: 12.5px;
  border-radius: 8px;
}

.node .dot {
  width: 7px;
  height: 7px;
  border-radius: 2px;
  background: #34d399;
}

.node.hl {
  border-color: #34d399;
  background: rgb(16 185 129 / 0.16);
  color: #6ee7b7;
}

.node.small {
  font-size: 11.5px;
  padding: 7px 10px;
}

.node em {
  font-style: normal;
  color: #8b8b95;
}

.line {
  position: relative;
  width: 1px;
  height: 44px;
  background: repeating-linear-gradient(#52525b 0 4px, transparent 4px 8px);
}

.line span {
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  white-space: nowrap;
  font-family: var(--vp-font-family-mono);
  font-size: 11px;
  color: #8b8b95;
}

.branches {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px;
  margin-top: 22px;
  padding-top: 22px;
  border-top: 1px dashed #3f3f46;
}

@media (max-width: 860px) {
  .flow-diagram {
    grid-template-columns: 1fr;
  }
}
</style>
