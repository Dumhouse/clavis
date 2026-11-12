import { defineConfig } from 'vitepress'

import { faq } from './faq'

const base = process.env.DOCS_BASE || '/'
const hostname = (process.env.DOCS_HOSTNAME || '').replace(/\/$/, '')
const absolute = (path) => (hostname ? `${hostname}${base}${path.replace(/^\//, '')}` : `${base}${path.replace(/^\//, '')}`)

export default defineConfig({
  title: 'Clavis',
  titleTemplate: ':title — Clavis',
  base,
  description: 'Open-source secrets management for humans, machines, and AI agents.',
  ignoreDeadLinks: true,
  appearance: true,
  lastUpdated: true,
  transformHead({ pageData }) {
    if (pageData.relativePath !== 'index.md') return
    const data = {
      '@context': 'https://schema.org',
      '@type': 'FAQPage',
      mainEntity: faq.map((f) => ({
        '@type': 'Question',
        name: f.q,
        acceptedAnswer: { '@type': 'Answer', text: f.a },
      })),
    }
    return [['script', { type: 'application/ld+json' }, JSON.stringify(data)]]
  },
  sitemap: hostname ? { hostname: `${hostname}${base}` } : undefined,
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: `${base}brand/logo-mark.svg` }],
    ['meta', { name: 'theme-color', content: '#ffffff', media: '(prefers-color-scheme: light)' }],
    ['meta', { name: 'theme-color', content: '#0a0a0c', media: '(prefers-color-scheme: dark)' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'Clavis' }],
    [
      'meta',
      {
        property: 'og:title',
        content: 'Clavis — secrets management for humans, machines, and AI agents',
      },
    ],
    [
      'meta',
      {
        property: 'og:description',
        content:
          'One Go binary, typed config, dynamic credentials, JIT access, and agent-safe tools. Open source, self-hosted, no feature gates.',
      },
    ],
    ['meta', { property: 'og:image', content: absolute('brand/social-card.png') }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
    ['meta', { name: 'twitter:image', content: absolute('brand/social-card.png') }],
  ],
  themeConfig: {
    logo: '/brand/logo-mark.svg',
    siteTitle: 'Clavis',
    nav: [
      { text: 'Docs', link: '/quickstart', activeMatch: '^/(quickstart|ci|architecture|self-hosting|migrations)' },
      { text: 'CLI', link: '/commands', activeMatch: '^/commands' },
      { text: 'Agents', link: '/agents', activeMatch: '^/agents' },
      { text: 'Security', link: '/security', activeMatch: '^/(security|supply-chain|reproducible-builds|vulnerability-disclosure)' },
      { text: 'Tour', link: '/screenshots', activeMatch: '^/screenshots' },
      {
        text: 'Community',
        items: [
          { text: 'Discussions', link: 'https://github.com/dumhouse/clavis/discussions' },
          { text: 'Issues', link: 'https://github.com/dumhouse/clavis/issues' },
          { text: 'Releases', link: 'https://github.com/dumhouse/clavis/releases' },
          { text: 'Contributing', link: '/community' },
        ],
      },
    ],
    sidebar: [
      {
        text: 'Get started',
        items: [
          { text: 'Quickstart', link: '/quickstart' },
          { text: 'Product tour', link: '/screenshots' },
          { text: 'CI and machine tokens', link: '/ci' },
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
      {
        text: 'Project',
        items: [
          { text: 'API stability', link: '/api-stability' },
          { text: 'Community', link: '/community' },
          { text: 'Security policy', link: '/vulnerability-disclosure' },
          { text: 'Sustainability', link: '/sustainability' },
        ],
      },
    ],
    outline: { level: [2, 3], label: 'On this page' },
    socialLinks: [{ icon: 'github', link: 'https://github.com/dumhouse/clavis' }],
    editLink: {
      pattern: 'https://github.com/dumhouse/clavis/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
    footer: {
      message: 'Released under the Apache-2.0 license.',
      copyright: 'Clavis — open source, self-hosted, no feature gates.',
    },
    search: { provider: 'local' },
    docFooter: { prev: 'Previous', next: 'Next' },
    externalLinkIcon: true,
  },
})
