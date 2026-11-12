# Product Design & UX

## Design principles

1. **Calm and confident.** Security tools shouldn't feel scary. Use neutral surfaces, one accent color, and generous spacing.
2. **Fast.** Every common action takes ≤ 2 clicks or one shortcut. Updates appear instantly (optimistic UI with rollback).
3. **Show, don't configure.** Sensible defaults; advanced options are hidden behind "Advanced", never removed.
4. **Explain consequences.** Before a risky action, say exactly what will happen ("3 secrets in **production** will change. 2 syncs will run.").
5. **Keyboard-first, mouse-friendly.** ⌘K for everything; shortcuts listed in menus.
6. **Accessible.** WCAG 2.2 AA: contrast, focus rings, screen reader labels, reduced motion.

## Visual language

- **Inspiration:** Linear, Vercel, Raycast, Resend.
- **Typography:** *Geist Sans* for UI, *Geist Mono* for keys, values, and code (self-hosted fonts, no Google Fonts CDN).
- **Color:** zinc neutrals; one accent (proposal: emerald/teal, "unlocked"). Semantic colors: production = amber badge, protected = lock icon, danger = red only for destructive actions.
- **Environment type colors** are used consistently in the UI, CLI, and Slack messages: development = blue, preview = violet, staging = teal, production = amber.
- **Item kinds:** secret = masked with key icon, variable = plain text with `{x}` icon, file = paperclip icon with filename and size.

> **Mockup:** see [design/mockups/index.html](https://github.com/dumhouse/clavis/blob/main/project/design/mockups/index.html) for a clickable prototype: secrets grid + drawer, promotion, access matrix with explainer, requests inbox (change / access / agent command), health, shared sets with blast radius, offboarding, CLI, ⌘K palette, and Add secret with generator. A **Style** switcher shows both shipped appearances in dark and light.

## Appearances

Clavis ships two appearances. Each works in dark and light mode, so users get four combinations. The choice is a per-user setting (Settings → Appearance), and an org can set the default for new members.

| Appearance | Character | Default |
|---|---|---|
| **Classic** | Zinc neutrals, emerald accent, Geist. Linear/Vercel-like, dense. | **Yes** (dark, or follows system) |
| **Warm** | Stone neutrals, sage accent, Manrope, larger radii (16/10 px) and taller rows (56 px). Friendlier for non-engineers. | Optional |

Implementation rules:
- Both appearances are **token sets only** (colors, fonts, radii, row height). Layout, components, and behavior are identical, so there is one UI to build and test.
- Tokens live in CSS variables keyed by `data-theme` (dark/light) and `data-style` (classic/warm) on `<html>`.
- Semantic colors (env types, danger, success, warnings) keep the same hue in both appearances, so "production = amber" never changes meaning.
- Every combination must pass WCAG 2.2 AA contrast; the CI visual tests run all four.
- The CLI is unaffected (it follows terminal colors).
- **Themes:** dark and light are both first-class; follows system by default.
- **Density:** comfortable by default, compact toggle for power users (the secrets grid).
- **Motion:** subtle 120–200 ms transitions via Motion; no motion when `prefers-reduced-motion`.
- **Icons:** Lucide.
- **Brand:** a simple key/keyhole monogram; logo work in M6.

## Information architecture

```
Org switcher ▾
├── Projects (home)
│   └── Project
│       ├── Secrets (grid)         ← default view
│       ├── Environments
│       ├── Schema
│       ├── Integrations (syncs)
│       ├── Dynamic secrets
│       ├── Access (members & identities for this project)
│       ├── Activity (project audit)
│       └── Settings
├── Shared sets (org-level secrets imported by many projects)
├── Health
├── Requests (access & change approvals)
├── Audit log
├── Members & Groups
├── Identities (machines & agents)
└── Org settings (general, SSO & SCIM, security policy, notifications)
```

## Key screens

### 1. First-run setup wizard
A guided 5-step flow: enter the one-time setup token, create admin (passkey prompt), create org (with environment template), create first project (optional `.env` drop), connect CLI. It then shows a "Get secrets into your app" card with a copy-paste CLI snippet for the user's OS. A progress check turns green live (via SSE) when the CLI first connects.

### 2. Secrets grid (the hero screen)
- Rows = keys; columns = environments (dev | staging | prod | …), sticky key column.
- Values masked; click or `Space` to reveal (audited), `C` to copy.
- Cell states: set, **missing** (dashed outline + "Add"), **inherited** (dimmed with parent badge), **from shared set** (set chip, e.g. `stripe`), **overlay** (sub-column, e.g. `prod · eu`), **overridden personally** (avatar dot), **reference** (link chip `→ prod.DB_HOST`), **invalid per schema** (red underline with tooltip), **expiring soon** (clock badge).
- Variables show values in plain text; files show filename + size with download/preview.
- Column headers show env type color, lock icon if protected, snowflake icon during a freeze.
- **Paste a `.env`** anywhere: it is parsed into a preview diff ("12 new, 3 changed") and applied in one batch.
- Inline edit with multi-line support; the `Unsaved changes` bar collects edits → **Review & save** shows a diff before commit.
- Filter/search (⌘F), tags, folder tree sidebar, compare mode (pick 2 envs, show only differences).
- Protected envs: edits create a **change request** instead of saving directly.

### 3. Secret detail drawer
Value with reveal/copy, comment, tags, schema info, **version history timeline** with diff and one-click restore, provenance ("inherited from dev"), "Used by" (syncs, identities, last read), and rotation status.

### 4. Environments
Card list with drag-to-reorder, parent/child tree, protected toggle, branch pattern, and a "Create branch env" action.

### 5. Access & roles
Matrix view: members/groups/identities × environments, showing effective permission (none / names / use / read / write). The custom role editor uses checkboxes grouped by resource with plain-English summaries. "Effective access" explainer: *"Why can Alex read prod?" → via group Backend → role Developer.*

### 6. Requests
Inbox of access and change requests with diff, requester, reason, and duration. Approve/deny with a comment. Slack/email notifications link here.

### 7. Audit log
Timeline with filters (actor, action, resource, time), JSON detail panel, export, and a chain-integrity badge ("Verified ✓").

### 8. Health dashboard
Score per project with cards: stale secrets (>90 days unchanged), unused (no reads in 90 days), duplicated values across projects, failing syncs, members without MFA, tokens without expiry, leaked secrets detected.

### 9. Integrations
- **Active syncs** table on top: target, environment, status (Synced / Drift / Failing) and a one-line reason.
- **Gallery by category:** Sync targets, Kubernetes, CI identity, Notifications, Audit & SIEM, Developer tools. Badges: Connected / New / Coming soon.
- **Sync page:**
  - **Drift card** shows the target value vs the Clavis value (masked) and who changed it. Actions: *Overwrite target*, *Import into Clavis* (opens a change request on protected envs), *Stop managing this key*.
  - **Failure card** gives the plain-language cause and numbered fix steps.
  - Also shows mapping (env/overlay → target path), run history, auth method (assume-role / OIDC preferred), behavior toggles (respect freezes, drift detection, delete-missing **off by default**, include variables) and an owner team for alerts.
- A new sync always shows a dry-run diff before its first push.

### 14. Projects home
Project cards (env chips, health score, missing count, members, last activity), a getting-started checklist that disappears when done, and an org activity feed.

### 15. Schema
Table of keys with type, required-per-env markers (D/S/P), rule and description, plus status per key. A problems banner mirrors `clavis check`. Typed config panel shows generated TS / Go / Python with a copyable `clavis codegen` command.

### 16. Identities
List of machines, integrations and agents. Machine detail: OIDC issuer and conditions, token lifetime, per-env access, and a warning plus "Switch to OIDC" for static non-expiring tokens. Agent detail: use-only badge, editable command allowlist, pending command requests and recent runs (including rejected ones).

### 17. Settings (org and project)
Sectioned page with a sticky sub-nav: General, Security policies, SSO/SCIM, Appearance (Classic/Warm preview cards), Notifications, Audit export, Danger zone. Project settings cover branch/preview environments, personal overrides, retention, duplicate detection and transfer/delete.

### 18. Sign in
Passkey first, then SSO, then email/password. Shows the server host and version.

### 10. Promote
Pick source → target environment. A diff table groups keys into **Added**, **Changed**, **Missing in source**, **Unchanged** (collapsed). Checkboxes select what to copy; schema checks run inline. The button reads "Promote 4 changes" or, for protected targets, "Request approval for 4 changes".

### 11. Shared sets
Org-level list (e.g. `stripe`, `sentry`, `shared-postgres`) showing which projects/environments import each set. Editing a set shows the blast radius before saving ("Affects 7 environments in 4 projects, 3 syncs will run").

### 12. Offboarding assistant
Triggered when a member is removed. It shows: sessions and tokens revoked ✓, then a list of secrets the person revealed or exported (newest first, grouped by project and env), each with owner, runbook link, and a "Rotate" or "Mark as safe" action. A progress bar tracks completion.

### 13. Generate secret
Dialog from "Add secret" or `clavis secrets generate`: type (password, API key, hex, base64, UUID, Ed25519/RSA keypair, JWT secret), length, character sets, live preview with entropy indicator.

## Interaction details

- **⌘K command palette:** jump to project/env/secret, run actions ("Create secret", "Invite member", "Switch org"), and search docs.
- **Shortcuts:** `G then S` secrets, `G then R` requests, `G then H` health, `G then A` access, `N` new secret, `/` search, `?` shortcuts & commands sheet, `J`/`K` next/previous in lists.
- **Page hints** (two layers, same content source):
  1. **Hint bar**, a 34 px status bar at the bottom of every page with the page's top 3 shortcuts, a rotating tip, and the **CLI equivalent** of what the page does (with copy button). The `›` button cycles tips. It can be hidden with `×`, and turned back on from the sheet or Settings → Appearance.
  2. **Shortcuts & commands sheet** (`?`, also in ⌘K) with this page's shortcuts, global shortcuts, CLI commands with descriptions and copy buttons, and all tips for the page.
  - Content is defined per page in one registry (`page → {keys, cmds, tips}`) shared by the bar, the sheet, ⌘K, and the docs site, so they never drift. CI checks that every CLI command shown exists in the CLI reference.
  - Hints never contain secret values; commands use placeholders or key names only.
  - The bar is on by default for the first 30 days per user, then auto-collapses to just `? Shortcuts` (reopenable).
- **Undo:** non-destructive changes show an "Undo" toast for 8 s.
- **Empty states:** every empty screen explains the concept in one sentence and offers the primary action plus a CLI equivalent.
- **Errors:** human-readable with a next step; a "Copy details" button includes a request ID.
- **Loading:** skeletons, no spinners for < 300 ms.
- **Real-time:** collaborators' edits appear live; presence avatars show who else is viewing a project.

## Copy & tone

- Plain, short, friendly; no blame ("Couldn't reach GitHub. Check the token has `repo` scope.").
- Use the same nouns everywhere: *Project, Environment, Folder, Secret, Identity, Sync*.

## CLI design

See [cli.md](cli.md). The same principles apply: beautiful by default, scriptable with `--json`, clear next steps in every error.
