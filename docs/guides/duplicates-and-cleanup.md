---
description: "Find secrets that share a value, see which environments are copies of each other, and delete environments you no longer need."
---

# Duplicates, look-alike environments and cleanup

Over time the same credential gets pasted under two names, staging keeps
production's database password, and an environment nobody uses sticks around.
These commands find and remove that.

## Find repeated values

```sh
clavis secrets duplicates
```

```text
SAME VALUE UNDER DIFFERENT KEYS
ENVIRONMENT  KEYS
dev          STRIPE_BACKUP, STRIPE_KEY

SAME KEY, SAME VALUE IN SEVERAL ENVIRONMENTS
KEY         ENVIRONMENTS         
STRIPE_KEY  dev, production      shared with production
```

It reports two things:

- **One value under different keys** in an environment: usually one credential
  copied under two names. Keep one and point the other at it.
- **One key with the same value in several environments**: flagged when
  **production** is one of them, because a leak from dev would then expose
  production. Give each environment its own credential.

Only secrets of kind `secret` with at least 6 characters are compared, so
settings such as `true` or `3000` do not show up. Values are fetched, compared
on your computer, and never printed. Reading them is recorded in the audit log
like any other reveal. In the web app the same check is on the **Compare**
tab, under **Repeated values and look-alike environments**.

Use `--only-env production` to look inside one environment, and `--json` for
scripts.

## See which environments are copies

```sh
clavis env similar
```

```text
PAIR               SAME VALUES  IDENTICAL  SAME KEYS  ONLY IN A / B
dev ↔ staging      92%          11 of 12   80%        1 / 2
staging ↔ production  8%        1 of 12    75%        2 / 1
```

**Same values** is how many of the keys both environments have hold an
identical value. A high number between two environments that should differ
(staging and production, say) means one is a copy of the other.

## Compare two environments key by key

```sh
clavis env diff dev production
```

lists keys that exist in only one environment or have a different value. See
[`clavis promote`](../reference/promote) to copy values across.

## Delete an environment

```sh
clavis env rm qa
```

This deletes the environment with its secrets, their history and personal
overrides. It cannot be undone, so you type the environment's slug to confirm;
in a script, pass `--yes`. A project must keep at least one environment, and
you need permission to manage environments. The deletion is recorded in the
audit log. In the web app, use **Delete** on the environment's card.

Before you delete, `clavis env similar` and `clavis secrets duplicates` show
whether anything in it is used elsewhere.

## Find secrets that leaked into git

```sh
clavis scan --history
```

reads every line any commit on any branch ever added, so a secret that was
committed and later deleted is still found. A secret in git history is already
exposed: rotate it first, then consider rewriting history.
