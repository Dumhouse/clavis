# Architecture

## Key decisions

| Area | Decision | Why | Rejected alternatives |
|---|---|---|---|
| Server language | **Go** (latest stable) | Single static binary, strong stdlib crypto, best ecosystem for cloud/K8s integrations, fast to build | Rust (slower iteration, no code sharing with CLI written in Go), Node/TS (heavier runtime, larger attack surface) |
| CLI language | **Go**, same module as server | Shares API client, types, crypto, and config parsing with the server | Rust (would duplicate client/crypto code) |
| HTTP / API framework | **Huma v2** on `chi` router | Code-first OpenAPI 3.1 generation, validation, RFC 9457 errors | gin/echo (no first-class OpenAPI), gRPC (unfriendly for web and curl users) |
| Database | **SQLite first** (M0–M2), **PostgreSQL added in M3** for HA; both supported at 1.0 | SQLite gives zero-setup self-hosting and halves early migration/testing work; Postgres arrives with the Helm chart and multi-replica support | Both from day one (doubles early work), Postgres-only (adds setup friction), MySQL (no extra value) |
| DB access | **sqlc** (type-safe generated queries) + **goose** migrations, one migration dir per dialect, behind a dialect-neutral `store` interface | Explicit SQL, no ORM magic, compile-time checked; the interface keeps Postgres a drop-in addition | ent/GORM (hidden queries, harder to audit) |
| Background jobs | Built-in DB-backed queue (`jobs` table, `SELECT … FOR UPDATE SKIP LOCKED` on Postgres; single worker on SQLite) | No Redis dependency | Redis/asynq, river (Postgres only) |
| Real-time updates | **Server-Sent Events** | Simple, works through proxies, enough for one-way updates | WebSockets (bidirectional not needed) |
| Web UI | **React + TypeScript + Vite**, **TanStack Router/Query/Table**, **Tailwind CSS v4**, **shadcn/ui** (Radix), **Motion** | Modern look without a dedicated designer, accessible primitives, SPA embeds into the Go binary | Next.js (needs a Node server, breaks the single-binary model), Svelte (smaller component ecosystem) |
| API client for web | Generated from OpenAPI via **openapi-typescript + openapi-fetch** | Server is the single source of truth | Hand-written clients |
| CLI framework | **Cobra** + Charm **lipgloss / huh / bubbles** | Standard, beautiful prompts and tables | urfave/cli |
| Auth | Server-side sessions (opaque token, HttpOnly cookie) for humans; short-lived access tokens for machines | Revocable instantly, no JWT pitfalls | Stateless JWT sessions |
| Config | Env vars + optional YAML file, validated at startup | 12-factor, Docker-friendly | — |
| Releases | **GoReleaser**, cosign signing, SBOM, SLSA provenance | Supply-chain integrity | — |
| License | **Apache-2.0** with DCO sign-off | Permissive and patent grant; maximum adoption | AGPL (deters companies), open core (against our principles) |

## System overview

```
                   ┌──────────────────────────────────────────────┐
  Browser ───────▶ │                clavis server                 │
  (embedded SPA)   │  ┌────────┐  ┌──────────┐  ┌──────────────┐  │
                   │  │ HTTP   │─▶│ Services │─▶│ Store (sqlc) │──┼──▶ SQLite / PostgreSQL
  CLI / SDKs ────▶ │  │ (Huma) │  │ + AuthZ  │  └──────────────┘  │
  K8s operator ──▶ │  └────────┘  │ + Audit  │  ┌──────────────┐  │
  GitHub Action ─▶ │       │      └──────────┘─▶│ Crypto/Keys  │──┼──▶ Root key provider
  MCP clients ───▶ │       ▼            │       └──────────────┘  │    (file/env, AWS/GCP/Azure KMS)
                   │  ┌────────┐  ┌──────────┐                    │
                   │  │  SSE   │◀─│ Event bus│◀─ Job workers ─────┼──▶ Sync targets (GitHub, Vercel,
                   │  └────────┘  └──────────┘   (syncs, rotation,│    AWS SM, …), webhooks, email
                   │                              dynamic leases) │
                   └──────────────────────────────────────────────┘
```

- **Stateless server.** All state lives in the DB, so you can run N replicas behind a load balancer (Postgres mode).
- **Job workers** run in the same binary (`clavis server` runs API + workers; `--role=api|worker` splits them for scale).
- **Event bus** is in-process. With multiple replicas it uses Postgres `LISTEN/NOTIFY` to fan out SSE events.

### Integration providers

Every sync target is a small Go package implementing one interface, so adding a provider never touches the core:

```go
type Provider interface {
    Validate(ctx context.Context, cfg Config) error                    // check credentials and permissions, return fix hints
    Diff(ctx context.Context, cfg Config, want []Item) (Plan, error)    // create / update / delete / drift
    Push(ctx context.Context, cfg Config, plan Plan) (Result, error)
    Delete(ctx context.Context, cfg Config, keys []string) error
}
```

Rules every provider follows:
- **Prefer federation over stored credentials:** AWS assume-role, GCP workload identity, GitHub App. Stored tokens are encrypted like secrets and never shown again.
- **Dry run first:** a new sync shows its `Plan` and needs confirmation before the first push. `clavis syncs run <id> --dry-run` prints the same plan.
- **Drift detection:** a periodic `Diff` (default 5 min) flags target values changed outside Clavis. Drift pauses that key until a human picks a side.
- **Safe defaults:** syncs never delete keys they did not create unless "delete keys not in Clavis" is on, which it is not by default.
- **Policy-aware:** syncs respect freezes and protected environments. Importing a target value into a protected env opens a change request.
- **Plain-language errors:** providers map API errors to a cause plus numbered fix steps (e.g. missing GitHub App permission).
- **Outbound calls** go through the SSRF-safe HTTP client (M2-33). Generic HTTP sync signs payloads with HMAC.
- Every run is a job with retries and backoff, and is audited as `sync.push` / `sync.drift` / `sync.fail`.

## Repository layout

```
clavis/
├── cmd/
│   └── clavis/            # single entrypoint: `clavis server`, `clavis run`, … (CLI and server in one binary)
├── internal/
│   ├── server/            # HTTP wiring, middleware, SSE, embedded SPA serving
│   ├── api/               # Huma handlers grouped by resource (v1)
│   ├── service/           # business logic (secrets, projects, members, syncs, …)
│   ├── store/             # sqlc-generated queries + migrations (postgres/, sqlite/)
│   ├── crypto/            # envelope encryption, key hierarchy, root key providers
│   ├── authn/             # passwords, sessions, MFA, passkeys, OIDC/SAML, machine auth
│   ├── authz/             # permission engine
│   ├── audit/             # hash-chained audit log, exporters
│   ├── jobs/              # queue + workers
│   ├── integrations/      # sync providers (github, vercel, aws, gcp, …)
│   ├── dynamic/           # dynamic secret engines (postgres, mysql, aws)
│   └── cli/               # Cobra commands, local cache, keyring, masking
├── pkg/
│   ├── client/            # public Go API client (used by CLI, operator, terraform provider)
│   └── schema/            # secret schema types and validation (shared)
├── web/                   # React SPA (built output embedded via go:embed)
├── operator/              # Kubernetes operator (separate Go module)
├── integrations/
│   ├── github-action/
│   ├── terraform-provider/
│   └── vscode/
├── deploy/                # Dockerfile, docker-compose, Helm chart
├── docs/                  # these planning docs → later the docs site source
└── tasks/                 # milestone task lists
```

One binary contains both server and CLI. `brew install clavis` gives developers the CLI; `docker run clavis/clavis server` runs the server. Same version, no drift.

## API design

- **REST + JSON**, versioned under `/api/v1`. OpenAPI 3.1 spec served at `/api/v1/openapi.json` and rendered docs at `/docs/api`.
- **Errors:** RFC 9457 `application/problem+json` with stable `type` codes.
- **Pagination:** cursor-based (`?cursor=…&limit=…`).
- **Concurrency:** `ETag` / `If-Match` on secrets and settings to prevent lost updates in the UI.
- **Idempotency:** `Idempotency-Key` header on POSTs that create resources.
- **Bulk operations:** `PATCH /secrets:batch` for grid edits (atomic, single audit event group).
- **Rate limits:** per-identity token bucket, `RateLimit-*` headers.

### Main resources

```
/auth/*                         login, logout, mfa, passkeys, oidc, saml, device-code, machine token exchange
/orgs/{org}
/orgs/{org}/members, /invites, /roles, /groups, /identities (machine/agent), /audit-events
/orgs/{org}/secret-sets/{set}   shared secrets imported by many environments
/orgs/{org}/env-templates, /offboarding/{user}
/orgs/{org}/projects/{project}
  /environments/{env}           (+ /overlays, /freezes, :clone)
  /secrets                      ?env=&overlay=&path=&resolve=true|false&include=overrides&kind=
  /promotions                   diff + apply between environments
  /secrets/{key}/versions
  /snapshots                    point-in-time recovery
  /schema                       secret schema definitions
  /syncs, /webhooks, /dynamic-secrets, /rotations, /access-requests, /change-requests
/share-links                    one-time secret sharing
/system/*                       health, readiness, metrics (Prometheus), setup wizard
```

## Deployment modes

| Mode | Command | Use |
|---|---|---|
| **Single binary** | `clavis server` | Homelab, small teams. SQLite file + key file in a data dir. |
| **Docker** | `docker run -p 8080:8080 -v clavis:/data clavis/clavis` | Same as above, containerized. |
| **Docker Compose** | `deploy/compose.yaml` | Postgres + Clavis + optional Caddy for TLS. |
| **Kubernetes (Helm)** | `helm install clavis clavis/clavis` | HA: N replicas, external Postgres, KMS root key. |

TLS: built-in automatic HTTPS via ACME (CertMagic) as an option, or behind any reverse proxy.

## Observability

- Structured JSON logs (`slog`) with **automatic redaction** of secret values and tokens.
- Prometheus metrics at `/metrics` (opt-in auth).
- OpenTelemetry tracing (opt-in).
- `/healthz` (liveness) and `/readyz` (DB + root key available).

## Testing strategy

- Unit tests for crypto, authz, schema, and resolution logic (table-driven, high coverage).
- Integration tests against SQLite from M0; against **both** SQLite and Postgres (testcontainers) from M3.
- Authorization coverage test: every route must declare a permission (from M0).
- API contract tests generated from OpenAPI.
- E2E tests for the web UI with **Playwright**; CLI golden-output tests.
- Fuzzing (`go test -fuzz`) for parsers: `.env` import, references, schema, token formats.
- Security CI: `govulncheck`, `gosec`, CodeQL, dependency review, `npm audit`, container scanning (Trivy).
