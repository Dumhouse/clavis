---
description: "Stop secrets from reaching git: run clavis scan on staged files from a pre-commit hook, with or without the pre-commit framework."
---

# Catch leaked secrets before commit

`clavis scan --staged` checks the files you are about to commit for Clavis
tokens. With `--values` it also matches the values of your own secrets, which
needs permission to read them. It exits with code 7 when it finds something,
which stops the commit.

## With the pre-commit framework

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/dumhouse/clavis
    rev: v0.3.3
    hooks:
      - id: clavis-scan
```

```sh
pre-commit install
```

Use a release tag for `rev`. The hook needs the `clavis` CLI on your `PATH`
([install it](../download)). To match secret values too, override the entry:
`args: []` and `entry: clavis scan --staged --values` in your own config.

## Without it

```sh
clavis scan --install-hook
```

This writes `.git/hooks/pre-commit`, which runs the same check. It won't
overwrite a hook you already have.

## In CI

Run `clavis scan` (without `--staged`) in a pipeline step to check the whole
tree; the exit code is the same.
