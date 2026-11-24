# Migrate from Bitwarden Secrets Manager

The importer calls the Bitwarden Secrets Manager CLI (`bws`), so it uses the
login you already have. Install `bws` as described in
[Bitwarden's CLI docs](https://bitwarden.com/help/secrets-manager-cli/) and
export an access token that can read the project:

```sh
export BWS_ACCESS_TOKEN=0.abc...
```

Preview the import, then apply it. `--remote-project` takes the project ID from
`bws project list`:

```sh
clavis import --provider bitwarden \
  --remote-project 4f2a1c8e-0000-0000-0000-000000000000 --dry-run

clavis import --provider bitwarden \
  --remote-project 4f2a1c8e-0000-0000-0000-000000000000
```

Every secret becomes a `secret` keyed by its Bitwarden key. Values stay in
Bitwarden until you cut over. Use `--map` to rename Bitwarden-specific keys,
`--folder` to place them under one folder, and `--remote-project` again to
import another project into the same Clavis environment.
