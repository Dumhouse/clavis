# Self-hosting

## Docker

The fastest way to a working setup is the [install wizard](./install): pick
where Clavis runs and how it does HTTPS, and copy the compose file.

### Plain HTTP (localhost or behind a proxy)

```sh
docker run -d --name clavis --restart unless-stopped \
  -p 80:8080 \
  -v clavis-data:/data \
  dumhouse/clavis:latest
```

### HTTPS built in

```sh
docker run -d --name clavis --restart unless-stopped \
  -p 80:8080 -p 443:8443 \
  -v clavis-data:/data \
  -e CLAVIS_TLS=self-signed \
  -e CLAVIS_SERVER_BASE_URL=https://192.168.1.20 \
  dumhouse/clavis:latest
```

`CLAVIS_TLS` is `off` (default), `self-signed`, `file` (your certificate) or
`acme` (Let's Encrypt, also for a LAN-only server through the DNS challenge
with `CLAVIS_ACME_DNS`). Port 80 then only redirects to HTTPS. With
`self-signed`, the CLI shows the certificate fingerprint and asks each person
once (`docker exec clavis clavis admin tls-info` prints it for you to share).
Every mode, trusting a self-signed certificate, and reverse-proxy examples are in
[HTTPS and certificates](./https).

### Ports

Inside the container Clavis listens on **8080** (HTTP) and **8443** (HTTPS),
because it runs as a non-root user. Publish them on the standard host ports
with `-p 80:8080 -p 443:8443`, or on any other ports you like.

### Docker Compose

```yaml
services:
  clavis:
    image: dumhouse/clavis:latest
    container_name: clavis
    restart: unless-stopped
    ports:
      - "80:8080"
      - "443:8443"
    environment:
      CLAVIS_TLS: "self-signed"
      CLAVIS_SERVER_BASE_URL: "https://192.168.1.20"
      # Optional: claim the server with your own token and email.
      # CLAVIS_SETUP_TOKEN: "<at least 24 random characters>"
      # CLAVIS_SETUP_OWNER_EMAIL: "you@example.com"
    volumes:
      - clavis-data:/data

volumes:
  clavis-data:
```

```sh
docker compose up -d
docker compose logs clavis | grep setup_url   # the setup link with the token
```

`restart: unless-stopped` restarts Clavis after a crash, a Docker restart or a
reboot. A deliberate `docker compose stop` is respected.

### First-run setup

On first start the log prints a `setup_url` with the one-time token built in.
Open it to create the owner account. You can also print the token while setup
is open:

```sh
docker exec clavis clavis setup-token
```

| Variable | Meaning |
| --- | --- |
| `CLAVIS_SETUP_TOKEN` | Use this token instead of a random one (at least 24 characters). Handy for compose files and automation: you know the token before the server starts, so the link is `https://<host>/setup#token=<token>`. |
| `CLAVIS_SETUP_OWNER_EMAIL` | Only this email can create the owner account; the setup page fills it in. |

Back up the root key file (`/data/root.key` by default) separately from
database backups.

### Plain HTTP on a network address

Browsers only keep Clavis's secure session cookie over HTTPS or on
`localhost`. If you still open Clavis at `http://<ip>`, it falls back to
non-Secure cookies so sign-in works, and logs a warning. Your password and
secrets then cross the network unencrypted and passkeys are unavailable: use it
for a short trial only.

### Locked out, and no email configured

Password resets are normally sent by email. Without SMTP, an admin on the
server prints a one-time reset link instead; it works for the only owner too:

```sh
docker exec clavis clavis admin reset-password you@example.com
```

The link is valid for 24 hours (`--valid-for 1h` to shorten it) and the reset
is recorded in the audit log. See
[`clavis admin reset-password`](./reference/admin-reset-password) and the other
[admin commands](./reference/admin).

## Kubernetes

See [Install with Helm](./helm.md) for OCI install and upgrade commands, chart
values, database configuration, persistence, ingress, and root key backup. The
chart defaults to one SQLite-backed replica; use PostgreSQL for multiple
replicas.

## Postgres

Use a managed Postgres with TLS, automated backups, and PITR. Clavis uses
`LISTEN/NOTIFY` for multi-replica events and `SKIP LOCKED` for jobs.

## Root key

Choose one:

- `file`: mounted secret volume, 0600; back it up separately.
- `env`: injected by the platform secret manager.
- `aws-kms` / `gcp-kms`: no root key at rest in the app.
- Shamir sealing: `clavis admin shamir split --seal`; unseal before start.

## Backups

Manual backup and restore:

```sh
clavis admin backup --out clavis-backup.enc
clavis admin restore clavis-backup.enc --yes
```

Scheduled backups run inside the server when enabled:

```yaml
backup:
  enabled: true
  dir: "/var/lib/clavis/backups"
  interval: 24h
  keep: 7
```

Each run writes an encrypted `clavis-<timestamp>.clvbk` file (0600) and prunes
all but the newest `keep` files. The same superuser is required to restore, so
keep the root key and the backups in separate trust domains. Test a restore
quarterly.

## Upgrades

1. Snapshot the database.
2. Read the release notes and verify signatures.
3. Run migrations by starting the new binary once.
4. Roll API replicas, then workers.
5. Run `clavis health` and `clavis admin audit verify`.

## Reverse proxy

Terminate TLS at the proxy, forward `X-Forwarded-For`, and keep request body
limits aligned with `server.max_body_bytes`. Do not strip the `Authorization`
header or cookies.
