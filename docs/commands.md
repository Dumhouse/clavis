# Command reference

Every command in the `clavis` binary. Run `clavis <command> --help` for the
same information in your terminal.

Global flags work on every command:

| Flag | Description |
| --- | --- |
| `--server URL` | Clavis server URL (default: `.clavis.yaml` or saved login) |
| `--org SLUG` | Organization slug |
| `--project SLUG` | Project slug |
| `--env SLUG` | Environment slug |
| `--path PATH` | Folder path (default `/`) |
| `--json` | Machine-readable JSON output |
| `--debug` | Debug logging |

---

## Getting started

### `clavis login`

Sign in with the browser device-code flow. The token is stored in the OS
keychain (or `CLAVIS_CREDENTIAL_FILE` when set).

```sh
clavis login --server https://clavis.example.com
clavis login --no-browser
```

### `clavis logout`

Revoke the saved session and delete the local credential.

### `clavis whoami`

Show the signed-in user and the organizations they belong to.

### `clavis init`

Link the current directory to an organization/project/environment and write
`.clavis.yaml`. Offers to import an existing `.env` file.

```sh
clavis init
clavis init --project-name "My App"
```

### `clavis link` / `clavis unlink`

Change the project linked to this directory, or remove `.clavis.yaml`.

```sh
clavis link --project web-app --env staging
clavis unlink
```

---

## Secrets

### `clavis secrets ls`

List key names. Values stay hidden unless `--reveal` is passed.

```sh
clavis secrets ls
clavis secrets ls --reveal
```

### `clavis secrets get KEY`

Print one secret value. The reveal is audited.

```sh
clavis secrets get DATABASE_URL
```

### `clavis secrets set KEY [VALUE]`

Create or update a secret. Omit `VALUE` to be prompted with hidden input, or
pass `-` to read from stdin.

```sh
clavis secrets set DATABASE_URL
clavis secrets set API_KEY - < key.txt
clavis secrets set FEATURE_FLAG true --kind variable --message "enable beta"
```

Flags: `--kind secret|variable|file`, `--comment`, `--message`, `--yes`.

### `clavis secrets generate KEY`

Generate a random value and store it.

```sh
clavis secrets generate SESSION_KEY --type password --length 48
clavis secrets generate RELEASE_ID --type uuid
```

Types: `password`, `apikey`, `hex`, `base64`, `uuid`.
Flags: `--kind`, `--length`, `--message`, `--yes`.

### `clavis secrets rm KEY`

Soft-delete a secret. History is kept.

```sh
clavis secrets rm OLD_KEY --yes
```

### `clavis secrets history KEY`

Show the version history for a key, including change messages.

### `clavis secrets rollback KEY --version N`

Create a new version from an older one.

```sh
clavis secrets rollback DATABASE_URL --version 4
```

### `clavis secrets override KEY [VALUE]`

Set a personal override for the current user. Overrides are allowed in
non-production environments only.

```sh
clavis secrets override DATABASE_URL
clavis secrets override DATABASE_URL --remove
```

Flags: `--remove`, `--comment`.

---

## Running and exporting

### `clavis --overlay SLUG`

Tenant/region overlays override selected keys without changing the base
environment. Pass `--overlay` to `run`, `export`, `secrets ls`, or
`secrets get`:

```sh
clavis --overlay eu run -- npm start
clavis --overlay us-east export --format dotenv
clavis --overlay eu secrets ls --reveal
```

Overlays are stored separately from base values, resolve after inheritance,
and appear as extra columns in the secrets grid. Create them in the UI/API and
set values with the overlay endpoints.

### `clavis run -- COMMAND [ARGS...]`

Run a command with secrets injected as environment variables. Output is masked
against the injected values.

```sh
clavis run -- npm run dev
clavis run --mount -- ./start.sh   # file secrets become private temp files
clavis run --no-mask -- ./debug.sh # explicit opt-out of masking
clavis run --watch -- pnpm dev      # restart when secrets change
```

`--watch` subscribes to the organization event stream and restarts the child
when the linked environment's secrets change. The child is restarted with a
fresh value set.

### Offline cache

When `.clavis.yaml` has `cache: true` (written by `clavis init`), `clavis run`
stores an AES-256-GCM encrypted copy of the resolved environment after a
successful fetch. If the server is unreachable, it uses that cache for up to 24
hours and prints a warning. The cache key lives in the OS keychain (or the
0600 credential fallback), and the cache is scoped to server/org/project/env.

Set `cache: false` in `.clavis.yaml` to opt out for a project. Clear the cache
with your platform's cache directory or `CLAVIS_CACHE_DIR` if you set one.

### `clavis shell`

Start an interactive subshell with the environment's resolved secrets exported
and a colored prompt. Values are not masked; use it only in a trusted terminal.

```sh
clavis shell
```

`--mount` writes file secrets to a `0700` temp directory as `0600` files and
removes the directory on exit. The environment variable contains the file path.

### `clavis export`

Export an environment.

```sh
clavis export                                  # dotenv to stdout
clavis export --format docker > .env           # Docker env-file
clavis export --format json -o backup.json
clavis export --variables-only                 # no read-values permission needed
clavis export --format k8s > secret.yaml
```

Formats: `dotenv`, `docker`, `json`, `yaml`, `shell`, `k8s`.

### `clavis share KEY`

Create a client-encrypted one-time share link. Only ciphertext is uploaded; the
AES key stays in the URL fragment and never reaches the server.

```sh
clavis share DATABASE_URL
clavis share API_KEY --ttl 2h --views 3
clavis share API_KEY --prompt-password
```

The server enforces expiry and view limits, and checks an optional password
hash. The browser viewer at `/share/<id>#<key>` decrypts locally and shows the
value once. Share links are intended for people, not for automation.

### `clavis scan [PATH]`

Scan files for valid Clavis token formats and, with `secrets:read-values`, for
resolved secret values in memory. Findings never include the value itself.

```sh
clavis scan
clavis scan ./config
clavis scan --staged
clavis scan --install-hook
```

Exit code `7` means a leak was found. `--install-hook` writes a pre-commit hook
that runs `clavis scan --staged`. `--values=false` skips value matching when
only token formats should be checked.

### `clavis import FILE|DIR`

Import `.env`, JSON, or YAML. A directory is walked recursively and each file's
parent directory becomes the secret folder.

```sh
clavis import .env
clavis import ./config/ --kind variable
clavis import ./secrets/ --dry-run
clavis import .env --map import-map.yaml
```

Dry runs classify keys as `new`, `changed`, `unchanged`, or `present` (when the
token cannot reveal values) and never print values.

Map keys and folders before applying:

```yaml
keys:
  OLD_DATABASE_URL: DATABASE_URL
  DROP_ME: ""
folders:
  "": /imported
  legacy: /legacy
```

#### Provider imports

Use `--provider` to pull from an external secret manager. Provider tokens and
endpoints can also come from environment variables.

```sh
# Infisical
clavis import --provider infisical \
  --remote-project "$INFISICAL_PROJECT" --remote-env dev \
  --token "$INFISICAL_TOKEN" --dry-run

# Doppler
clavis import --provider doppler \
  --remote-project my-project --remote-env dev \
  --token "$DOPPLER_TOKEN" --dry-run

# Vault or OpenBao KV v1/v2
clavis import --provider vault \
  --base-url "$VAULT_ADDR" --vault-mount secret --vault-version 2 \
  --remote-path apps/web --token "$VAULT_TOKEN" --dry-run

# 1Password Connect
clavis import --provider onepassword \
  --base-url "$OP_CONNECT_HOST" --op-vault "$OP_VAULT" \
  --token "$OP_CONNECT_TOKEN" --dry-run

# AWS Secrets Manager
clavis import --provider aws --aws-region eu-central-1 \
  --aws-access-key "$AWS_ACCESS_KEY_ID" --aws-secret-key "$AWS_SECRET_ACCESS_KEY" \
  --aws-prefix app/ --dry-run

# GCP Secret Manager
clavis import --provider gcp --gcp-project my-project \
  --gcp-service-account service-account.json --gcp-prefix APP_ --dry-run
```

Provider environment variables: `INFISICAL_TOKEN`, `INFISICAL_URL`,
`INFISICAL_PROJECT`, `INFISICAL_ENV`, `DOPPLER_TOKEN`, `DOPPLER_PROJECT`,
`DOPPLER_CONFIG`, `VAULT_ADDR`, `VAULT_TOKEN`, `OPENBAO_ADDR`, `OPENBAO_TOKEN`,
`OP_CONNECT_HOST`, `OP_CONNECT_TOKEN`, `OP_VAULT`, `AWS_REGION`,
`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`,
`GCP_PROJECT`, and `GOOGLE_APPLICATION_CREDENTIALS`.

The web UI’s **Import** dialog accepts `.env` or flat JSON and lets you remap
keys, folders, and kinds with the same dry-run diff before applying.

---

## Schema and validation

Keep the typed configuration contract in the repository so reviews and CI
catch bad values before deployment.

### `clavis check`

Validates the selected environment against the project schema. Missing required
keys, type mismatches, pattern failures, and invalid enum values are errors.
Optional missing keys and keys absent from the schema are warnings.

```sh
clavis check
clavis check --env production
clavis check --all
clavis check --json
```

Exit code `6` means at least one check failed. Use it directly in CI:

```sh
clavis check --env production
```

### `clavis schema ls`

List the project schema.

```sh
clavis schema ls
clavis schema ls --json
```

### `clavis schema pull`

Download the schema as a YAML file suitable for code review. The default file is
`clavis.schema.yaml`; `-o -` writes to stdout.

```sh
clavis schema pull
clavis schema pull -o config/clavis.schema.yaml
```

### `clavis schema push [FILE]`

Replace the project schema from a YAML file. The server validates keys, types,
patterns, required maps, and enum values before saving.

```sh
clavis schema push
clavis schema push config/clavis.schema.yaml --dry-run
```

### `clavis schema infer`

Suggest a schema from the current environment’s resolved values. The output
contains types and kinds, never secret values. Review the suggestions before
committing or pushing them.

```sh
clavis schema infer
clavis schema infer -o clavis.schema.yaml
clavis schema infer --push
```

Schema entries support `string`, `url`, `int`, `bool`, `json`, and `enum` types;
`secret`, `variable`, and `file` kinds; per-environment-type `required` flags;
a regular-expression `pattern`; a description, an example, and enum values.

### `clavis codegen`

Generate a typed config loader from the schema.

```sh
clavis codegen --lang ts -o src/config.ts
clavis codegen --lang go --package config -o internal/config/config.go
clavis codegen --lang python -o app/config.py
```

- TypeScript uses zod and exports a schema, inferred type, and `loadConfig`.
- Go exports a struct, `Load`, `LoadFor(envType)`, and `LoadFrom(environ)`.
- Python uses pydantic and exports `Config` and `load_config`.

Generated loaders parse env-style strings: integers become numbers, booleans
accept `true/false/1/0/yes/no`, JSON fields are decoded, enums are checked, and
patterns are enforced. Keys required in any environment type are required
fields; the rest are optional. Go’s `LoadFor` additionally enforces the required
set for the environment type you pass.

---

## Environments

### `clavis env ls`

List environments in the linked project.

### `clavis env create NAME`

Create an environment.

```sh
clavis env create QA --slug qa --type staging
```

Types: `development`, `preview`, `staging`, `production`.

### `clavis env branch [BRANCH]`

Create or refresh a preview environment for a git branch. Branch environments
inherit from the first development environment, get a stable slug derived from
the branch name, and expire after the organization's preview retention period.
Calling the command again refreshes the expiry.

```sh
clavis env branch
clavis env branch feature/checkout
clavis --env dev-feature-checkout-abc123 secrets ls
```

Compare key names between two environments.

```sh
clavis env diff dev prod
```

### `clavis promote`

Diff two environments, select keys, schema-check them, snapshot the target, and
copy the selected values. Statuses are `added`, `changed`, `missing` (present
only in the target), and `unchanged`.

```sh
clavis promote --from dev --to staging
clavis promote --from dev --to staging --keys DATABASE_URL,STRIPE_KEY
clavis promote --from dev --to staging --interactive
clavis promote --from dev --to staging --request
```

- New and changed keys are selected by default.
- Schema errors block promotion.
- The target is snapshotted before writing unless `--snapshot=false`.
- Protected targets require an Admin/Owner. Other users should pass `--request`;
  an Admin or Owner can approve it in the **Promote** page.

---

## Organization and access

### `clavis members ls`

List organization members and their roles.

### `clavis access explain <user|group>`

Explain why a member or group does or does not have access.

```sh
clavis access explain ada@example.com --permission secrets:write --env prod
clavis access explain platform-team --permission secrets:read-values --project web-app
```

Flags: `--permission`, `--project`, `--environment`, `--path`, `--user`,
`--group`.

---

## Identities

### `clavis identities ls`

List machine and workload identities.

### `clavis identities create NAME`

Create a machine or agent identity and its first token.

```sh
clavis identities create deploy-bot --role Developer --project web-app --identity-env prod
clavis identities create github-actions \
  --oidc github --repo acme/repo --branch main --gh-environment production \
  --role Developer --project web-app --identity-env dev
clavis identities create ci \
  --oidc generic --issuer https://idp.example.com --audience clavis \
  --claim ref=refs/heads/main
```

Flags: `--type machine|agent`, `--role`, `--project`, `--identity-env`,
`--oidc github|gitlab|generic`, `--issuer`, `--audience`, `--repo`, `--branch`,
`--gh-environment`, `--k8s-namespace`, `--k8s-service-account`,
`--gcp-service-account`, `--aws-account`, `--aws-arn`, `--aws-user-id`,
`--claim key=value` (repeatable).

For AWS IAM roles, configure account/ARN trust and replay a SigV4-signed STS
`GetCallerIdentity` request to `POST /api/v1/auth/aws`:

```sh
clavis identities create aws-ci --aws-account 123456789012 \
  --aws-arn 'arn:aws:iam::123456789012:role/github-*' \
  --project web-app --identity-env dev
```

For Kubernetes service account tokens, pass the cluster issuer, audience, and
namespace/service account:

```sh
clavis identities create k8s-deployer \
  --oidc kubernetes --issuer https://kubernetes.default.svc.cluster.local \
  --audience clavis --k8s-namespace apps --k8s-service-account deployer
```

For GCP service account ID tokens:

```sh
clavis identities create gcp-deployer \
  --oidc gcp --gcp-service-account deployer@project.iam.gserviceaccount.com
```

For GitLab, `--repo` sets the `project_path` claim:

```sh
clavis identities create gitlab-ci \
  --oidc gitlab --repo group/project --branch main --audience clavis
```

### `clavis identities revoke NAME`

Delete an identity and its tokens.

---

## Migration guides

See [migrations/](migrations/) for Infisical, Doppler, Vault/OpenBao, and `.env`
import walkthroughs.

## Dynamic secrets

Issue short-lived PostgreSQL credentials instead of static passwords. Leases
have a default and maximum TTL, can be renewed or revoked, are revoked on
expiry, and are revoked when the creating identity is deleted.

### `clavis lease config-create`

```sh
clavis lease config-create   --host db.internal --database app --username clavis_admin   --password "$PGPASSWORD" --grant-role app_readwrite   --default-ttl 3600 --max-ttl 86400
```

### AWS STS config

```sh
clavis lease config-create --engine aws \
  --aws-region eu-central-1 --aws-access-key "$AWS_ACCESS_KEY_ID" \
  --aws-secret-key "$AWS_SECRET_ACCESS_KEY" --aws-role-arn arn:aws:iam::123456789012:role/deploy
```

AWS leases return `access_key_id`, `secret_access_key`, `session_token`, and
`expiration`. STS credentials cannot be renewed; create a new lease instead.

### `clavis lease configs`

List dynamic secret configs.

### `clavis lease create CONFIG_ID`

Issue credentials. The password is shown once.

```sh
clavis lease create 4a1c… --ttl 1800
```

### `clavis lease ls CONFIG_ID`

List lease metadata and status.

### `clavis lease renew LEASE_ID --ttl 3600`

Extend a lease up to the config's maximum TTL.

### `clavis lease revoke LEASE_ID`

Drop the generated database role and mark the lease revoked.

### `clavis lease run CONFIG_ID -- COMMAND`

Issue credentials, export `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, and
`PGDATABASE`, run the command, then revoke the lease even if the command fails.

## Offboarding

Removing a member immediately revokes sessions, personal tokens, project
grants, and dynamic leases. Clavis also builds an offboarding review from the
secrets the member revealed, with progress and rotate/mark-safe actions.

### `clavis members offboarding USER`

Show the review. Use `--start` to create tasks from reveal history.

```sh
clavis members offboarding ada@example.com --start
```

### `clavis members mark-safe USER TASK_ID`

Mark a revealed secret reviewed as safe.

### `clavis members mark-rotated USER TASK_ID`

Mark a revealed secret as rotated. The Members page has the same review dialog.

## Health

### `clavis health`

Report stale/unused secrets, missing rotation policies, expiring secrets,
members without MFA, and non-expiring identity tokens. Enable
`duplicate_detection` in organization settings to compare value fingerprints
without exposing values.

```sh
clavis health
clavis health --project web-app
clavis health --json
```

The web **Health** page shows the same score and findings.

## Just-in-time access

Users can request a temporary role for one environment. Requests need one or
more approvers, a reason, and a duration of 5 minutes to 24 hours. Approving
creates a time-bound project grant. Requesters cannot approve their own request.

### `clavis access request`

```sh
clavis access request --env prod --role Developer --duration 1h \
  --reason "Debugging checkout latency" --approvals 1
```

### `clavis access requests`

List pending requests. Non-managers see only their own requests.

```sh
clavis access requests
clavis access requests --status approved
```

### `clavis access approve|reject REQUEST_ID`

Approve or reject a request. The final approval creates the grant.

Creating a request emails Admin/Owner approvers and, if configured, posts signed
Approve/Reject buttons to Slack, Teams, or Discord webhooks. Links expire after
72 hours. Opening a link requires sign-in and password re-authentication before
the decision is applied; requesters still cannot approve themselves.

### `clavis access review`

Report every member and group that can read values in an environment. Use
`--format csv` for access-review spreadsheets.

```sh
clavis access review --env prod
clavis access review --env prod --format csv > access-review.csv
```

### `clavis access break-glass`

Immediately grant an Admin-level grant for a short incident window, lift any
active freeze, and write a loud audit event. Requires `settings:manage`.

```sh
clavis access break-glass --env prod --reason "SEV-1 checkout outage" --duration 1h
```

## Change freezes

### `clavis env freeze`

Schedule a freeze that blocks secret writes, promotions, syncs, and overlay
edits for the linked environment.

```sh
clavis env freeze --reason "Release freeze" --hours 4
clavis env freeze --start 2026-11-15T09:00:00Z --hours 8 --reason "Black Friday"
```

### `clavis env freezes`

List upcoming and active freeze windows.

### `clavis env unfreeze FREEZE_ID`

Delete a freeze window. Break-glass access also lifts the active freeze.

## Rotation

Two-phase rotation creates the new credential, writes and verifies the Clavis
secret, then revokes the old credential. Failures roll back where the provider
supports it, and every phase is recorded.

### `clavis rotation create KEY`

```sh
clavis rotation create DATABASE_PASSWORD --provider postgres \
  --pg-host db.internal --pg-database app --pg-username admin \
  --pg-password "$PGPASSWORD" --pg-role app_user --interval 24h
```

Providers: `postgres` (role password), `aws` (IAM access key; the secret stores
JSON with `access_key_id` and `secret_access_key`), and `webhook`
(`create`/`rollback`/`revoke` actions with optional HMAC).

### `clavis rotation status`

Show managed policies, unmanaged secrets, and secrets older than the stale
threshold. The server emails owner groups weekly about unmanaged secrets that
have not changed for 90 days.

```sh
clavis rotation status
clavis rotation status --stale-days 60
```

### `clavis rotation ls`

List policies, interval, next run, and last status.

### `clavis rotation run POLICY_ID`

Rotate immediately.

### `clavis rotation enable|disable POLICY_ID`

Toggle the schedule.

### `clavis rotation runs POLICY_ID`

Show rotation phase history and errors.

### `clavis rotation rm POLICY_ID`

Delete a policy.

The **Rotation** page shows schedules, next runs, status, errors, run history,
and one-click manual rotation.

## AI agents

Agent identity tokens use the `clv_at_` prefix. They receive only
`secrets:read-names` and `secrets:use`, expire after one day by default (maximum
seven days), and are denied staging and production. They never receive values
directly: values are returned only for commands on the server-side allowlist.

### `clavis agents ls`

List agent identities and whether their allowlist is deny-all.

```sh
clavis agents ls
```

### `clavis agents allow IDENTITY COMMAND...`

Approve command patterns. Patterns support `*` and `?`; a bare `*` is rejected.

```sh
clavis agents allow coding-agent "npm test"
clavis agents allow coding-agent "go test ./..." "pnpm dev"
```

### `clavis agents revoke IDENTITY [COMMAND...]`

Remove patterns, or use `--clear` to return to deny-all.

```sh
clavis agents revoke coding-agent "pnpm dev"
clavis agents revoke coding-agent --clear
```

Agents cannot change their own allowlist. Every allowed run and every denial is
audited, and the Identities page shows recent agent activity.

### `clavis mcp`

Run a Model Context Protocol server over stdio for Claude Code, Cursor, Codex,
Windsurf, or any MCP client.

```sh
clavis mcp --agent claude-code              # recommended: uses your clavis login
CLAVIS_TOKEN=clv_at_… clavis mcp            # headless agents only
```

With `--agent NAME` (or `CLAVIS_AGENT`), `clavis mcp` uses your own login to
start a one-hour agent session, keeps it in memory and renews it. The MCP config
holds no secret, and logging out ends the session. You can only start an agent
with access you hold yourself. A static agent token in `CLAVIS_TOKEN` still
works for headless use and prints a warning.

Tools: `list_secrets`, `check_env`, `run_with_secrets`, `request_secret`.
`run_with_secrets` checks the command against the allowlist before decrypting
values and masks command output. Masking reduces accidental leaks but is not a
security boundary: an allowed command can still exfiltrate the secrets it
receives.

---

## Shared sets

### `clavis sets ls`

List org-level shared sets.

### `clavis sets show SET [--reveal]`

Show a set and its keys. `--reveal` decrypts values.

```sh
clavis sets show shared-api --reveal
```

### `clavis sets create NAME --slug SLUG`

Create a shared set with its own encryption key.

### `clavis sets set SET KEY [VALUE]`

Create or update a setting inside a set.

```sh
clavis sets set shared-api STRIPE_KEY
```

### `clavis sets rm SET KEY`

Remove a key from a set.

### `clavis sets import SET [--env E]`

Import a set into an environment. Local environment values always win.

```sh
clavis sets import shared-api --env staging
```

### `clavis sets detach SET [--env E]`

Remove the import from an environment.

---

## Syncs

### `clavis syncs ls`

List syncs for the linked project.

### `clavis syncs create ENVIRONMENT`

Create a generic HTTP sync. The receiver gets a signed JSON POST.

```sh
clavis syncs create dev --endpoint https://deploy.example.com/hooks/clavis --secret "$SYNC_SECRET"
```

Flags: `--endpoint`, `--secret`, `--provider http|gitlab|github|vercel|aws|gcp|netlify|cloudflare`,
`--gitlab-url`, `--gitlab-project`, `--gitlab-token`, `--protected`, `--masked`,
`--github-repo`, `--github-scope`, `--github-environment`, `--github-token`,
`--vercel-project`, `--vercel-team`, `--vercel-token`, `--aws-region`,
`--aws-access-key`, `--aws-secret-key`, `--aws-session-token`, `--aws-service`,
`--aws-prefix`, `--gcp-project`, `--gcp-service-account-json`,
`--gcp-prefix`, `--netlify-account`, `--netlify-site`, `--netlify-token`,
`--cloudflare-account`, `--cloudflare-mode`, `--cloudflare-project`,
`--cloudflare-script`, `--cloudflare-token`, `--delete-missing`.

GitHub syncs can authenticate with a PAT or a GitHub App installation:

```sh
clavis syncs create dev --provider github --github-repo acme/repo \
  --github-app-id 42 --github-installation-id 123 \
  --github-private-key @app.private-key.pem
```

GitHub syncs encrypt secrets with the repository public key and create or
update Actions variables:

```sh
clavis syncs create dev --provider github --github-repo acme/repo \
  --github-token "$GITHUB_TOKEN"

clavis syncs create prod --provider github --github-scope environment \
  --github-repo acme/repo --github-environment production \
  --github-token "$GITHUB_TOKEN"
```

Netlify and Cloudflare syncs push environment variables and Worker/Pages
secrets:

```sh
clavis syncs create dev --provider netlify --netlify-account acme \
  --netlify-token "$NETLIFY_TOKEN"

clavis syncs create dev --provider cloudflare --cloudflare-account "$CF_ACCOUNT" \
  --cloudflare-mode workers --cloudflare-script my-worker --cloudflare-token "$CF_TOKEN"
```

GCP syncs create Secret Manager secrets and add versions using a service
account:

```sh
clavis syncs create dev --provider gcp --gcp-project my-project \
  --gcp-service-account-json @service-account.json
```

AWS syncs write SSM parameters or Secrets Manager secrets with SigV4:

```sh
clavis syncs create dev --provider aws --aws-region eu-central-1 \
  --aws-access-key "$AWS_ACCESS_KEY_ID" --aws-secret-key "$AWS_SECRET_ACCESS_KEY" \
  --aws-service ssm
```

Vercel syncs create or update project environment variables:

```sh
clavis syncs create dev --provider vercel --vercel-project prj_123 \
  --vercel-token "$VERCEL_TOKEN"
```

GitLab syncs upsert project CI/CD variables:

```sh
clavis syncs create dev --provider gitlab --gitlab-project group/project \
  --gitlab-token "$GITLAB_TOKEN" --masked
```

### `clavis syncs run ID`

Preview or execute a sync. Use `--dry-run` to preview; `--yes` confirms and
sends values.

```sh
clavis syncs run 123e4567 --dry-run
clavis syncs run 123e4567 --yes
```

### `clavis syncs behavior ID`

Change sync behavior toggles.

```sh
clavis syncs behavior 123e4567 --delete-missing
```

### `clavis syncs drift ID`

Compare local values with a readable remote provider. Supported for GitLab,
AWS, GCP, Netlify, Vercel, GitHub variables, and Cloudflare Pages.

```sh
clavis syncs drift 123e4567
```

### `clavis syncs logs ID`

Show recent run history, status, key count, and errors.

---

## Audit

### `clavis audit ls`

List recent audit events for the organization.

```sh
clavis audit ls
clavis audit ls --limit 200 --after 1234
clavis audit ls --json
```

Events never contain secret values. Agent command runs, denials, promotions,
allowlist changes, and expiry reminders are recorded here.

### `clavis admin audit verify`

Recompute the audit hash chain and verify signed checkpoints.

### `clavis admin audit checkpoint`

Sign the current audit chain head with the instance Ed25519 key.

```sh
clavis admin audit verify
clavis admin audit checkpoint
```

---

## Administration

### `clavis admin shamir split`

Split the root key into threshold shares and optionally remove the root key so
the server cannot start until unsealed.

```sh
clavis admin shamir split --shares 5 --threshold 3 --out ./shares --seal --yes
```

### `clavis admin unseal`

Reconstruct and verify the root key from shares, then remove the sealed marker.

```sh
clavis admin unseal --share share-01.key --share share-03.key --share share-05.key
```

Shamir unseal requires `keys.provider=file`. Shares are equivalent to the root
key; store them offline and separately.

### `clavis admin backup`

Create an AES-256-GCM encrypted, consistent database snapshot. SQLite uses
`VACUUM INTO`; Postgres uses `pg_dump`. The backup includes wrapped organization
keys, so keep the root key separately and protected.

```sh
clavis admin backup --out clavis-backup.enc
```

### `clavis admin restore`

Restore an encrypted snapshot while the server is stopped. The previous SQLite
database is moved aside with a timestamped suffix.

```sh
clavis admin restore clavis-backup.enc --yes
```

### `clavis admin rotate-root-key`

Initialise or rotate the root key while the server is stopped.

```sh
clavis admin rotate-root-key --init
clavis admin rotate-root-key            # prompts for confirmation
clavis admin rotate-root-key --yes      # unattended
clavis admin rotate-root-key --config /etc/clavis/clavis.yaml
```

The command re-wraps every org key, re-encrypts every TOTP secret with the new
root-derived MFA key, and writes a backup of the previous root key.

### `clavis admin migrate-db`

Copy a SQLite database into Postgres. The target is migrated automatically and
must be empty unless `--force` is passed.

```sh
clavis admin migrate-db --from ./data/clavis.db --to "$CLAVIS_DB_DSN"
clavis admin migrate-db --from ./data/clavis.db --to "$DSN" --force --yes
```

### `clavis server`

Run the API, job worker, and embedded web UI.

```sh
clavis server
clavis server --config /etc/clavis/clavis.yaml
clavis server --role=api       # HTTP only
clavis server --role=worker    # jobs only
clavis server --role=all       # default
```

---

## Shell completion

```sh
clavis completion bash
clavis completion zsh
clavis completion fish
clavis completion powershell
```

---

## Environment variables

| Variable | Purpose |
| --- | --- |
| `CLAVIS_SERVER` | Server URL |
| `CLAVIS_TOKEN` | CLI token for CI (overrides the stored credential) |
| `CLAVIS_ORG`, `CLAVIS_PROJECT`, `CLAVIS_ENV` | Default context |
| `CLAVIS_CREDENTIAL_FILE` | Override the credential file location |
| `CLAVIS_CONFIG` | Server configuration file |
| `CLAVIS_DATA_DIR` | Data directory |
| `CLAVIS_ROOT_KEY` | Root key for the `env` key provider |

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | General error |
| 2 | Usage error |
| 3 | Authentication required |
| 4 | Permission denied |
| 5 | Not found |
| 6 | Validation failed |
| 7 | Secret leak detected |
