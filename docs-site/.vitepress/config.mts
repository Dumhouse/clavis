import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Clavis',
  description: 'Open-source secrets management for humans, machines, and AI agents.',
  srcDir: '.docs',
  cleanUrls: true,
  ignoreDeadLinks: true,
  head: [['link', { rel: 'icon', href: '/brand/logo-mark.svg' }]],
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/quickstart' },
      { text: 'CLI', link: '/commands' },
      { text: 'Security', link: '/security' },
      { text: 'GitHub', link: 'https://github.com/dumhouse/clavis' },
    ],
    sidebar: [
      {
        text: 'Get started',
        items: [
          { text: 'Quickstart', link: '/quickstart' },
          { text: 'Concepts', link: '/architecture' },
          { text: 'CLI reference', link: '/commands' },
        ],
      },
      {
        text: 'Operations',
        items: [
          { text: 'Self-hosting', link: '/self-hosting' },
          { text: 'Security design', link: '/security' },
          { text: 'Supply chain', link: '/supply-chain' },
          { text: 'Reproducible builds', link: '/reproducible-builds' },
        ],
      },
      {
        text: 'Integrations',
        items: [
          { text: 'AI agents and MCP', link: '/agents' },
          { text: 'Migration guides', link: '/migrations/' },
          { text: 'Fuzzing', link: '/fuzzing' },
        ],
      },
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/dumhouse/clavis' }],
    search: { provider: 'local' },
  },
})
