# M6 — Hardening & Launch (`v1.0`)

Goal: trustworthy, documented, fast, and launched.

## Security assurance 🔒
- [ ] **M6-01** 🔒 Internal threat-model review against [security.md](../docs/security.md); close gaps (M)
- [ ] **M6-02** 🔒 External security audit (crypto, auth, authz, web, CLI); fix all critical/high; publish report (L)
- [ ] **M6-03** 🔒 Continuous fuzzing (OSS-Fuzz application) (S)
- [ ] **M6-04** 🔒 SLSA L3 provenance, reproducible-build verification docs, `clavis upgrade` signature verification (M)
- [ ] **M6-05** GitHub secret scanning partner program registration for `clv_` tokens (S)
- [ ] **M6-06** Vulnerability disclosure program (and bug bounty if funded) (S)

## Quality & performance
- [ ] **M6-07** Load tests (k6): 10k secrets/project, 1k req/s reads, publish results (M)
- [ ] **M6-08** Accessibility audit (WCAG 2.2 AA) and fixes (M)
- [ ] **M6-09** UI polish pass: animations, empty states, copy review, icons, onboarding checklist (M)
- [ ] **M6-10** Upgrade path tests (v0.x → v1.0 migrations on real data) (M)
- [ ] **M6-11** API stability review; freeze `/api/v1`; deprecation policy (S)

## Docs & website
- [ ] **M6-12** Docs site (Astro Starlight or Fumadocs): quickstart, concepts, CLI reference (generated), API reference (from OpenAPI), integrations, self-hosting, security whitepaper (L)
- [ ] **M6-13** Migration guides: from Infisical, Doppler, Vault/OpenBao, `.env` files (M)
- [ ] **M6-14** Landing page with comparison table and live demo instance (resets hourly) (M)
- [ ] **M6-15** Logo, brand assets, social images (S)

## Launch
- [ ] **M6-16** Public roadmap + GitHub Discussions + Discord/Matrix community (S)
- [ ] **M6-17** Launch posts: Show HN, Product Hunt, Reddit (r/selfhosted, r/devops, r/golang), dev.to, X/Bluesky (S)
- [ ] **M6-18** Listings: awesome-selfhosted, CNCF landscape, Homebrew core, Artifact Hub (S)
- [ ] **M6-19** Sustainability plan: GitHub Sponsors / Open Collective (no feature gating, ever) (S)
