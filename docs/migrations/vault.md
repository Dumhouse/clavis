# Migrate from Vault or OpenBao

```sh
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=...
clavis import --provider vault --vault-mount secret --vault-version 2 \
  --remote-path apps/web --dry-run

clavis import --provider vault --vault-mount secret --vault-version 2 \
  --remote-path apps/web --folder /vault
```

For the reverse direction during migration, create a Clavis `vault` sync that
writes one KV v2 secret per key at `<mount>/<path>/<KEY>`.
