# Migrate from .env files

```sh
clavis import .env --dry-run
clavis import .env --kind variable --map import-map.yaml
clavis import ./environments/dev/ --folder /imported
```

Directories are walked recursively and each file's parent directory becomes a
folder. Avoid committing `.env` files while migrating; add them to
`.gitignore` and use `clavis scan` to check for leaks.
