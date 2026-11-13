---
title: CLI commands
description: "The CLI reference moved to /reference/."
---

<script setup>
import { onMounted } from 'vue'
import { useRouter, withBase } from 'vitepress'

const router = useRouter()
onMounted(() => {
  const hash = window.location.hash.replace(/^#/, '')
  router.go(withBase('/reference/') + (hash ? `#${hash}` : ''))
})
</script>

# CLI commands

The CLI reference moved to **[CLI reference](./reference/)**: every command
has its own page with usage, flags and examples.
