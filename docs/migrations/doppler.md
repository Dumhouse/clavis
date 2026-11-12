# Migrate from Doppler

```sh
export DOPPLER_TOKEN=dp.st...
clavis import --provider doppler \
  --remote-project my-project --remote-env dev --dry-run

clavis import --provider doppler \
  --remote-project my-project --remote-env dev --folder /doppler
```

Use `--map` to rename Doppler-specific keys. Compare with
`clavis env diff dev staging` and promote through `clavis promote` rather than
copying values manually.
