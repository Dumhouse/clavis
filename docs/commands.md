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
`--oidc github|generic`, `--issuer`, `--audience`, `--repo`, `--branch`,
`--gh-environment`, `--claim key=value` (repeatable).

### `clavis identities revoke NAME`

Delete an identity and its tokens.

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

### `clavis server`

Run the API, job worker, and embedded web UI.

```sh
clavis server
clavis server --config /etc/clavis/clavis.yaml
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
