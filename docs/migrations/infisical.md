# Migrate from Infisical

1. Create a scoped machine identity in Clavis with `secrets:write` on the target
   environment.
2. Set `INFISICAL_TOKEN`, `INFISICAL_PROJECT`, and `INFISICAL_ENV`.
3. Preview and apply:

```sh
clavis import --provider infisical \
  --remote-project "$INFISICAL_PROJECT" --remote-env "$INFISICAL_ENV" \
  --dry-run

clavis import --provider infisical \
  --remote-project "$INFISICAL_PROJECT" --remote-env "$INFISICAL_ENV" \
  --folder /imported --map import-map.yaml
```

Use `clavis schema infer` after import, then `clavis check` before switching
applications. Move any shared values into Clavis shared sets and keep imports
scoped by environment.
