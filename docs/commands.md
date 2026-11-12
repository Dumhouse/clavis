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

### `clavis run -- COMMAND [ARGS...]`

Run a command with secrets injected as environment variables. Output is masked
against the injected values.

```sh
clavis run -- npm run dev
clavis run --mount -- ./start.sh   # file secrets become private temp files
clavis run --no-mask -- ./debug.sh # explicit opt-out of masking
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

### `clavis import FILE|DIR`

Import `.env`, JSON, or YAML. A directory is walked recursively and each file's
parent directory becomes the secret folder.

```sh
clavis import .env
clavis import ./config/ --kind variable
clavis import ./secrets/ --dry-run
```

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

### `clavis env diff SOURCE TARGET`

Compare key names between two environments.

```sh
clavis env diff dev prod
```

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
