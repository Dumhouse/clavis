---
description: "Use Clavis secrets on Vercel, Fly.io, Render and other hosts: push them with a sync, import them with the host's CLI, or fetch them at start-up."
---

# Secrets for Vercel, Fly.io, Render and other hosts

Hosts keep their own environment variables. There are three ways to keep them
in step with Clavis, from least to most work.

## Push: a sync (Vercel and others)

Clavis writes the values into the host whenever they change. Vercel, Netlify,
Cloudflare, AWS, GCP, GitHub, GitLab and Forgejo are supported:

```sh
clavis syncs create prod --provider vercel --vercel-project prj_123 \
  --vercel-token "$VERCEL_TOKEN"
```

Save the host's token once as a [platform token](./sync-connections) and reuse
it across syncs.

## Import with the host's CLI (Fly.io)

For a host with a command that reads `NAME=value` lines, pipe an export into
it. This is a one-time copy, so repeat it after you change a secret:

```sh
clavis export --env production --format dotenv | fly secrets import
```

The values end up stored by the host, and the export is recorded in
Clavis's audit log.

## Fetch at start-up (Render and anywhere with a shell)

Give the service only a machine token, and let Clavis fetch the rest when the
process starts. Nothing is copied to the host.

1. Create a token for the environment:

   ```sh
   clavis identities create render-prod --type machine --project web-app --identity-env production
   ```

2. Add `CLAVIS_TOKEN`, `CLAVIS_SERVER`, `CLAVIS_ORG`, `CLAVIS_PROJECT` and
   `CLAVIS_ENV` as the host's environment variables.
3. Install the CLI in your build step and start the app through it:

   ```sh
   # build command
   curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/main/scripts/install.sh \
     | CLAVIS_INSTALL_DIR="$PWD/bin" CLAVIS_VERSION=0.3.3 bash

   # start command
   ./bin/clavis run -- node server.js
   ```

If the Clavis server cannot be reached when the app starts, `clavis run`
cannot fetch its secrets and the app does not start. The encrypted offline
cache (`cache: true` in `.clavis.yaml`) only helps on hosts that keep their
disk between restarts.
