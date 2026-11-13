---
description: "Stop committing and passing around .env files. Move them into Clavis and run your app with secrets injected."
---

# Replace .env files

A `.env` file is a plain-text copy of your secrets that gets emailed, pasted
into chat, and committed by accident. Clavis keeps one encrypted copy per
environment and injects it when you run your app.

## 1. Import what you have

Preview first, then import:

```sh
clavis import .env --dry-run
clavis import .env
```

Use `--kind variable` for values that are not secret (log levels, feature
flags), and `--folder /imported` to keep a directory of files organized. More
options are in [Migrate from .env files](../migrations/dotenv).

## 2. Run with secrets injected

Link the repository once, then wrap any command:

```sh
clavis init
clavis run pnpm dev
```

Values are placed in the process environment, never written to disk, and
masked if the command prints them.

## 3. Delete the file and keep it out

```sh
rm .env
echo ".env" >> .gitignore
clavis scan
```

`clavis scan` looks for secrets that are already in your tree so you can rotate
anything that leaked.

## 4. Add a schema so mistakes fail early

Describe which keys each environment needs, then check in CI:

```sh
clavis check --env production
```

A missing or wrongly typed key fails the check before the bad config ships.

## Next

- [CI and machine tokens](../ci) for pipelines.
- [Quickstart](../quickstart) if you have not run a server yet.
