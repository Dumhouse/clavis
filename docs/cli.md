# CLI

Single Go binary `clavis` (same binary as the server). Installed via Homebrew, Scoop, winget, apt/rpm repos, `go install`, a curl install script (signed), and Docker.

## UX rules

- **Pretty by default, scriptable always:** colored tables and prompts on a TTY; `--json` / `--quiet` for scripts; colors disabled automatically when not a TTY or when `NO_COLOR` is set.
- **Every error suggests a fix:** `Not logged in. Run: clavis login`.
- **Context from the repo:** `.clavis.yaml` (committed, contains no secrets) stores org/project/default env, so commands need no flags inside a repo.
- **Non-interactive mode** in CI: auth from `CLAVIS_TOKEN` or OIDC auto-detection (GitHub Actions, GitLab, K8s).
- **Production safety:** when the target env is `production`, the CLI shows a colored banner; writes and destructive commands require typing the project name (or `--yes` plus a non-TTY, for CI). Orgs can block CLI writes to production entirely.
- **Stable exit codes:** 0 ok, 1 general error, 2 usage, 3 auth, 4 permission, 5 not found, 6 validation failed (`check`), 7 leak found (`scan`).

## Commands

```
clavis login [--server URL]           # device-code browser login; stores token in OS keychain
clavis logout
clavis whoami

clavis init                           # pick org/project/env, write .clavis.yaml, offer to import existing .env
clavis link / unlink                  # change project binding

clavis run [--env E] [--overlay O] [--path P] [--watch] [--mount] [--no-mask] -- <command>
                                      # inject secrets as env vars; masks output; --watch restarts on change;
                                      # --mount writes file secrets to a private temp dir, removed on exit
clavis shell [--env E]                # subshell with secrets loaded (prompt shows env in its type color)
clavis env ls | create | delete | clone <src> <dst> | diff <a> <b>
clavis env freeze <env> --until 2026-12-27 --reason "holidays" | unfreeze <env>
clavis promote <src> <dst> [--keys K1,K2] [--message "..."]   # interactive diff picker on TTY
clavis sets ls | show <set> | import <set> --env E | detach <set> --env E

clavis secrets ls [--env E] [--reveal]
clavis secrets get KEY [--plain]
clavis secrets set KEY [VALUE] [--kind secret|variable|file] [--message "why"] [--expires DATE]
                                      # prompts with hidden input if VALUE omitted; reads stdin with -
clavis secrets generate KEY [--type password|apikey|hex|uuid|ed25519|rsa|jwt] [--length N]
clavis secrets rm KEY
clavis secrets history KEY
clavis secrets rollback KEY --version N
clavis secrets rotate KEY [--env E]   # rotate via the configured rotation provider
clavis secrets override KEY           # personal override (non-protected envs)

clavis export [--format dotenv|json|yaml|shell|docker|k8s] [--out FILE]
clavis import FILE|DIR                # .env/json/yaml; a directory imports each file into its folder path
clavis import --from infisical|doppler|vault|openbao|1password|aws|gcp|dotenv

clavis check [--env E]                # validate against schema; non-zero exit on failure (CI)
clavis codegen --lang ts|go|python|rust [--out FILE]
clavis schema ls | edit | pull | push

clavis scan [--staged] [--install-hook]   # leak detection using real values + token formats

clavis share [KEY | --value -] [--views 1] [--expires 24h]   # one-time link

clavis request access --env prod --duration 1h --reason "..."
clavis request ls | approve ID | deny ID

clavis projects ls                    # projects you can access
clavis syncs ls                       # integrations/syncs with status (ok, drift, failing)
clavis syncs run <id> [--dry-run]     # run a sync now; --dry-run prints the diff without pushing
clavis syncs logs <id>                # recent runs and errors with suggested fixes

clavis audit ls [--project P] [--env E] [--actor A] [--action X] [--since 24h] [--json]

clavis identities ls
clavis identities create <name> [--oidc github|gitlab|k8s|aws|gcp] [--env E] [--expires 90d]
clavis identities revoke <name>
clavis agents allow '<pattern>' --identity <name>    # add a command to an agent's allowlist
clavis agents revoke '<pattern>' --identity <name>

clavis members ls [--json]            # members and their organization roles
clavis access explain <user|group> --permission secrets:write [--project P] [--env E] [--path /]
                                      # why someone has access (same engine as the UI explainer)
clavis health [--json]                # health report (same checks as the Health page)

clavis lease <dynamic-secret> [--ttl 1h] [-- <command>]   # dynamic creds, auto-revoked after command

clavis mcp                            # start MCP server (stdio) for AI agents (agent-safe mode)

clavis server [--config FILE]         # run the server
clavis admin  backup | restore | audit verify | rotate-root-key | unseal | offboard <user> | users ...

clavis completion bash|zsh|fish|powershell
clavis upgrade                        # self-update (verifies signature)
```

## `.clavis.yaml`

```yaml
server: https://clavis.example.com
org: acme
project: web-app
env: dev               # default env
path: /                # default folder
branch_envs: true      # map git branches to dev/<branch>
cache: true            # encrypted offline cache
```

## Agent-safe mode (`clavis mcp`)

The MCP server exposes tools to AI coding agents:

| Tool | Behavior |
|---|---|
| `list_secrets` | Returns key names, descriptions, and schema info. **Never values.** |
| `check_env` | Reports missing or invalid keys. |
| `run_with_secrets` | Runs an allowed command with secrets injected; output is masked. |
| `request_secret` | Asks a human to add a missing secret (creates a notification). |

It authenticates as an **agent identity** holding only `secrets:read-names` + `secrets:use`.

- **Command allowlist is required and deny-all by default.** A human approves patterns (e.g. `npm test`, `go test ./...`, `pnpm dev`) in the UI or with `clavis agents allow`. Allowlists are stored server-side on the identity, not only in `.clavis.yaml`, so the agent can't edit its own allowlist.
- Agents get development/preview environments by default; production requires an explicit org policy.
- Every call is audited with the agent's identity and the exact command.
- **Limitation (stated in docs and UI):** an allowed command still receives the secrets. If that command runs code the agent wrote (e.g. a test file), the agent could exfiltrate values. Masking is a convenience, not a security boundary. Prefer dynamic, short-lived credentials for agent workflows.
