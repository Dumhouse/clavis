/*
 * Landing-page FAQ. Shared by the page (theme/components/Landing.vue) and the
 * FAQPage structured data emitted in config.mts, so they never drift apart.
 * Answers are plain text: they are also used verbatim in JSON-LD.
 */
export const faq: { q: string; a: string; link?: { text: string; href: string } }[] = [
  {
    q: 'Is Clavis really free? What is the catch?',
    a: 'Everything is Apache-2.0, including SSO, RBAC, approvals, rotation, dynamic secrets, and agent features. There is no enterprise edition and no feature is held back for a paid tier. The project is funded through sponsorships and optional paid support, kept separate from the code.',
    link: { text: 'Sustainability policy', href: '/sustainability' },
  },
  {
    q: 'How is Clavis different from HashiCorp Vault or OpenBao?',
    a: 'Vault is a general-purpose security toolkit with a large plugin ecosystem. Clavis focuses on application secrets: one binary with an embedded UI, a setup wizard instead of an unseal ceremony by default, typed config with codegen, environment promotion, and a safe path for AI agents. If you need Vault’s PKI or transit engines, keep Vault; Clavis can sync to it.',
    link: { text: 'Migrating from Vault', href: '/migrations/vault' },
  },
  {
    q: 'Can AI coding agents see my secret values?',
    a: 'Not through Clavis. Agents get their own identity with read-names and use only, a deny-all command allowlist, and MCP tools that never return values. An allowed command does receive the secrets in its environment, so pair Clavis with your agent’s sandbox and keep production out of agent scope.',
    link: { text: 'What this protects, and what it does not', href: '/agents#what-this-protects-and-what-it-does-not' },
  },
  {
    q: 'What happens if I lose the root key?',
    a: 'Encrypted values cannot be recovered without it, which is the point of keeping it separate from the database. Back it up apart from database backups, or keep it in AWS KMS or GCP KMS, or split it into Shamir shares held by different people.',
    link: { text: 'Self-hosting guide', href: '/self-hosting' },
  },
  {
    q: 'Which database do I need?',
    a: 'None to start: Clavis uses an embedded SQLite database by default. Switch to PostgreSQL when you want high availability or several server replicas.',
    link: { text: 'Self-hosting guide', href: '/self-hosting' },
  },
  {
    q: 'Can I migrate from Infisical, Doppler, Vault, or .env files?',
    a: 'Yes. clavis import reads from Infisical, Doppler, Vault and OpenBao, 1Password Connect, AWS Secrets Manager, GCP Secret Manager, and .env files. Every import supports a dry run, and the source stays readable until you cut over.',
    link: { text: 'Migration guides', href: '/migrations/' },
  },
  {
    q: 'Does it work with Kubernetes?',
    a: 'Yes. There is an operator with a ClavisSecret custom resource, a Secrets Store CSI driver that mounts secrets as files, and a webhook provider for External Secrets Operator.',
  },
  {
    q: 'Is Clavis ready for production?',
    a: 'The feature set for 1.0 is complete and the project is in its pre-release hardening phase: security review, load testing, and release verification. Follow the releases on GitHub and read the security design before relying on it for critical systems.',
    link: { text: 'Roadmap', href: '/roadmap' },
  },
]
