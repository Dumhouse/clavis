# Clavis

> Open-source secrets management that is beautiful, secure, and completely free.
> Every feature included. No enterprise edition.

**Status:** M1 core MVP is implemented end to end. Setup-token authentication,
argon2id passwords, sessions/CSRF, password reset and email verification,
orgs/projects/environments, encrypted versioned secrets, scoped machine tokens,
invites and project-level access grants, hash-chained audit, SSE updates, a
React UI, and a full CLI are working. Remaining polish is tracked in
[tasks/M1-core-mvp.md](tasks/M1-core-mvp.md).

## Quick start

See [docs/quickstart.md](docs/quickstart.md).

```sh
make dev                 # server on :8080 + Vite hot reload
# or
make build && ./bin/clavis server
```

Data (SQLite database and the 0600 root key) defaults to `./data`. Back up the
root key separately from database backups: without it, ciphertexts cannot be
decrypted.


Clavis (Latin for *key*) stores your application secrets (API keys, database URLs, certificates), lets you share them safely with your team, injects them into your apps, CI, and Kubernetes, and gives AI coding agents safe access without revealing values.

## Why

| Problem today | Clavis |
|---|---|
| Infisical, Phase, Doppler lock SSO, RBAC, rotation, and approvals behind paid tiers | Everything is Apache-2.0 and free |
| Vault/OpenBao are powerful but hard to operate (unseal, HCL policies, weak UI) | One binary, setup wizard, no unseal ceremony by default |
| SOPS/dotenv files have no access control, audit, or revocation | Full RBAC, tamper-evident audit log, instant revocation |
| AI agents need secrets to run code but should never see them | Agent-safe "use without reveal" mode + MCP server |

## Principles

1. **Secure by default.** Safe defaults, no footguns, never log a secret.
2. **Polished.** Feels like Linear or Vercel, not like an admin panel.
3. **Simple to run.** `docker run` and you're done. Scales to HA when you need it.
4. **Everything free.** One license, one edition.
5. **Developer-first.** CLI and API are first-class, not afterthoughts.

## Planning documents

| Document | Contents |
|---|---|
| [docs/vision.md](docs/vision.md) | Positioning, competitor analysis, differentiators, non-goals |
| [docs/quickstart.md](docs/quickstart.md) | Run the server, claim the instance, store a secret, call the API |
| [docs/architecture.md](docs/architecture.md) | Tech stack decisions, components, repo layout, API design, deployment |
| [docs/data-model.md](docs/data-model.md) | Entities, secret resolution rules, versioning |
| [docs/security.md](docs/security.md) | Threat model, key hierarchy, crypto, auth, authz, audit, supply chain |
| [docs/design.md](docs/design.md) | Visual language, UX principles, key screens |
| [operator/README.md](operator/README.md) | Kubernetes operator and `ClavisSecret` CRD |
| [docs/commands.md](docs/commands.md) | Complete user command reference with examples |
| [docs/cli.md](docs/cli.md) | CLI UX rules and command documentation conventions |
| [docs/roadmap.md](docs/roadmap.md) | Milestones M0–M6 with exit criteria, cut list, backlog |
| [design/mockups/index.html](design/mockups/index.html) | Clickable UI mockup (open in a browser) |
| [tasks/](tasks/README.md) | Detailed task lists per milestone |

## License

Apache-2.0 (planned) for server, CLI, web UI, SDKs, operator, everything.
