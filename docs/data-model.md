# Data Model

## Hierarchy

```
Instance
└── Organization
    ├── Members (users) ── Roles ── Groups
    ├── Machine & agent identities ── auth methods (token, OIDC federation, K8s, AWS IAM)
    ├── Shared secret sets (e.g. "stripe", "sentry") ── imported by many environments
    ├── Environment templates (default env list for new projects)
    ├── Audit events (hash-chained)
    └── Project
        ├── Environments (type: development | preview | staging | production)
        │   ├── parent env (inheritance)          e.g. dev/feat-x → dev
        │   ├── overlays (region / tenant)        e.g. prod + eu
        │   ├── imported shared sets
        │   ├── freeze windows
        │   └── Folders (paths: /, /backend, /backend/db)
        │       └── Items (kind: secret | variable | file) ── Versions (with change message)
        │                                                  └── Personal overrides (per user)
        ├── Secret schema (key definitions shared across envs)
        ├── Snapshots (point-in-time)
        ├── Syncs, Webhooks, Dynamic secret configs, Rotations
        └── Access requests, Change requests, Promotions
```

## Core entities

| Entity | Key fields | Notes |
|---|---|---|
| `users` | id, email, name, avatar, password_hash (argon2id, nullable for SSO-only), mfa settings, created_at | Instance-wide; belong to orgs via memberships |
| `passkeys` | id, user_id, credential_id, public_key, sign_count, name | WebAuthn |
| `sessions` | id, user_id, token_hash, ip, user_agent, expires_at, last_seen_at, mfa_verified | Opaque tokens; only hashes stored |
| `orgs` | id, slug, name, settings, wrapped_org_key, key_version | |
| `memberships` | org_id, user_id, role_id, status, removed_at | `removed_at` triggers offboarding |
| `roles` | id, org_id (null = built-in), name, permissions (JSON) | Built-in: Owner, Admin, Developer, Viewer |
| `groups` | id, org_id, name, (SCIM-synced) | Roles can be assigned to groups |
| `role_bindings` | scope (org/project/set), subject (user/group/identity), role_id, env glob, path glob, expires_at | Supports temporary (JIT) grants |
| `identities` | id, org_id, name, type (machine/agent), auth methods (JSON), token TTLs, allowed_commands (agents) | |
| `access_tokens` | id, identity_id/user_id, token_hash, prefix, scopes, expires_at, last_used_at, last_used_ip | |
| `projects` | id, org_id, slug, name, wrapped_project_key, key_version | |
| `env_templates` | id, org_id, name, environments (JSON: slug, name, type, parent) | Applied when a project is created |
| `environments` | id, project_id, slug, name, **type**, color, parent_id, position, is_protected, branch_pattern, preview_source (PR ref), expires_at | Type sets default policies |
| `overlays` | id, environment_id, slug (e.g. `eu`, `customer-acme`), name | A small set of keys that override the base env |
| `freeze_windows` | id, environment_id, starts_at, ends_at, reason, created_by | |
| `secret_sets` | id, org_id, slug, name, wrapped_set_key, key_version | Org-level shared secrets |
| `secret_set_imports` | set_id, environment_id, position | Many environments can import one set |
| `folders` | id, environment_id, path | |
| `items` | id, env_id, overlay_id (nullable), folder_id, **kind** (secret/variable/file), key_ciphertext, key_blind_index, value_ciphertext, comment_ciphertext, tags, **owner_group_id**, **notes_ciphertext** (runbook), **provider_url**, **expires_at**, version, updated_at, deleted_at | Key names encrypted; blind index for uniqueness and lookup |
| `item_versions` | item_id, version, value_ciphertext, comment_ciphertext, **change_message**, actor, created_at | Immutable history |
| `item_overrides` | key_blind_index, env_id, user_id, value_ciphertext | Personal overrides (non-production only) |
| `secret_schema` | project_id, key, type, required (per env type), pattern, description, example, kind | Shared across environments |
| `snapshots` | id, env_id, created_at, reason, payload (encrypted) | Point-in-time recovery |
| `promotions` | id, project_id, source_env, target_env, selected keys, status, change_request_id | Promotion history |
| `audit_events` | id, org_id, seq, prev_hash, hash, actor, action, resource, ip, user_agent, metadata, created_at | Append-only, never contains secret values |
| `reveal_events` | user_id, item_id, version, channel (ui/cli/export), at | Feeds the offboarding assistant (also in audit log) |
| `syncs` | id, project_id, env_id, path, provider, config (encrypted), status, last_synced_at | |
| `dynamic_secret_configs` / `leases` | engine, config (encrypted), default/max TTL; lease id, expires_at, revoked_at | |
| `access_requests` / `change_requests` | requester, target, diff (encrypted), approvals, status, expires_at | |
| `share_links` | id, ciphertext, view_limit, expires_at, password_hash | Value encrypted with a key that lives only in the URL fragment |
| `jobs` | id, kind, payload, run_at, attempts, locked_by, locked_until | DB-backed queue |

## Item kinds

| Kind | Example | Masked in UI | Reveal audited | `use`-only identities | Export |
|---|---|---|---|---|---|
| `secret` | `STRIPE_SECRET_KEY` | Yes | Yes | Injected, never returned | Requires `read-values` |
| `variable` | `LOG_LEVEL`, `API_URL` | No | No | Returned in plain text | Free |
| `file` | `GCP_SERVICE_ACCOUNT` (JSON), TLS cert | Yes | Yes | Mounted as a temp file | Requires `read-values` |

All three are encrypted at rest identically.

## Environment types

| Type | Default policies |
|---|---|
| `development` | Personal overrides allowed, no approvals, agents allowed (with allowlist) |
| `preview` | Auto-created per pull request or git branch, inherits from `staging` or `development`, auto-expires |
| `staging` | No personal overrides, writes audited, agents opt-in |
| `production` | Protected, change requests required, reveal requires a reason, no personal overrides, CLI writes need typed confirmation, agents denied, shown in red/amber everywhere |

Policies are defaults. Admins can adjust them per environment, and the UI shows when an environment deviates from its type's defaults.

## Secret resolution

When a client requests secrets for `project/env[+overlay]/path` with `resolve=true`, layers are applied from **lowest to highest precedence** (later layers win):

1. **Shared secret sets** imported by the environment (in import order).
2. **Parent chain**, root first (e.g. `staging` → `preview/pr-123`).
3. **Folder imports** into the requested folder.
4. **The environment's own items.**
5. **Overlay** (e.g. `eu`), if requested.
6. **Personal override** of the calling human (non-production environments only).

Then:

7. **References** expand `${KEY}`, `${env.KEY}`, `${env.path.KEY}`, `${project.env.KEY}`, `${set:stripe.KEY}`. Max depth 10, cycle detection, and the caller must have permission on every referenced item.
8. **Schema validation** (optional, `validate=true`) reports missing required keys and pattern mismatches.

The result includes per-key **provenance** (set / parent / folder import / own / overlay / override / reference). The UI shows this as a small badge.

## Versioning & recovery

- Every write creates an immutable `item_versions` row with an optional **change message** ("why"). Rollback = a new version copying an old value.
- Deletes are soft (`deleted_at`) for 30 days (configurable), then purged by a job.
- **Snapshots** are taken automatically before bulk changes, promotions, and on a schedule. Restore creates new versions (history is preserved).

## Promotion

`dev → staging` (or any pair):

1. Compute a diff by key: **added**, **changed**, **missing in source**, **unchanged**. Values are compared server-side; only the diff category is shown unless the user has `read-values`.
2. The user selects keys to copy.
3. Schema checks run against the target environment type.
4. If the target is protected, a change request is created. Otherwise the change is applied atomically, with a snapshot first.

## Preview & branch environments

- **Preview (per PR):** the GitHub integration creates `preview/pr-<n>` when a PR opens (inheriting from the configured parent), keeps it in sync with the branch, and deletes it when the PR closes or merges.
- **Branch (CLI):** with `branch_envs: true`, `clavis run` on git branch `feat/x` resolves `dev/feat-x`, creating it on first use if the project allows. It inherits from `dev`.
- Both expire after N days of inactivity (configurable).

## Freeze windows

During a freeze, writes, promotions, and syncs that change the environment are blocked. Break-glass bypasses the freeze for a single change, with a required reason and admin notification.

## Expiry & ownership

- `expires_at` on an item (e.g. a vendor API key's expiry) triggers reminders to the **owner group** at 30, 7, and 1 days.
- `notes` (runbook) and `provider_url` appear in the secret drawer and in rotation and offboarding flows.
