# Clavis

Open-source secrets management for humans, machines, and AI agents. One Go
binary with an embedded web UI: typed config, dynamic database and cloud
credentials, time-limited access with approvals, an audit log, and an MCP
server that lets coding agents run commands without seeing secret values.

- Documentation: https://dumhouse.github.io/clavis/
- Source: https://github.com/Dumhouse/clavis
- License: Apache-2.0

## Quick start

```sh
docker run -d --name clavis --restart unless-stopped \
  -p 80:8080 -v clavis-data:/data \
  dumhouse/clavis:latest
```

Open http://localhost and claim the server. The log prints a setup link with
the one-time token built in:

```sh
docker logs clavis 2>&1 | grep setup_url
```

or print the token itself (prefix with `ssh user@host` for a remote server):

```sh
docker exec clavis clavis setup-token
```

- `-p 80:8080` publishes the web UI on port 80. Without `-p` the page is not
  reachable.
- `--restart unless-stopped` restarts Clavis after a crash or a reboot.
- The volume holds the SQLite database **and the root key**. Back up the root
  key separately from database backups: without it the secrets cannot be
  decrypted.

**Easiest:** the [install wizard](https://dumhouse.github.io/clavis/install) asks where Clavis runs and
writes a ready `compose.yaml`, including HTTPS and a clickable setup link.

## HTTPS

Browsers only keep the sign-in cookie over HTTPS or on `localhost`, so give
Clavis HTTPS as soon as you open it from another machine. Set `CLAVIS_TLS`:

| `CLAVIS_TLS` | For | Ports |
| --- | --- | --- |
| `off` (default) | localhost, or behind your reverse proxy | `-p 80:8080` |
| `self-signed` | home lab by IP or local name | `-p 80:8080 -p 443:8443` |
| `file` | your own certificate (`CLAVIS_TLS_CERT_FILE`, `CLAVIS_TLS_KEY_FILE`) | `-p 80:8080 -p 443:8443` |
| `acme` | Let's Encrypt for a public domain | `-p 80:8080 -p 443:8443` |
| `acme` + `CLAVIS_ACME_DNS=cloudflare\|duckdns` | Let's Encrypt for a LAN-only server (DNS challenge, `CLAVIS_ACME_DNS_TOKEN`) | `-p 80:8080 -p 443:8443` |

With any TLS mode set `CLAVIS_SERVER_BASE_URL=https://<host>`. Details,
certificate trust and reverse-proxy examples: [HTTPS and certificates](https://dumhouse.github.io/clavis/https).

## Docker Compose

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
      # Optional: preset the setup token and owner email
      # CLAVIS_SETUP_TOKEN: "<at least 24 random characters>"
      # CLAVIS_SETUP_OWNER_EMAIL: "you@example.com"
    volumes:
      - clavis-data:/data

volumes:
  clavis-data:
```

```sh
docker compose up -d
docker compose logs clavis | grep setup_url
```

## Tags

| Tag | Meaning |
| --- | --- |
| `latest` | The most recent release |
| `X.Y.Z` | A specific release, for example `0.2.2` |

Images are multi-arch (`linux/amd64`, `linux/arm64`), distroless, and run as a
non-root user. The same images are published to `ghcr.io/dumhouse/clavis`.

## Configuration

The image runs `clavis server` with `CLAVIS_SERVER_ADDR=:8080` (HTTPS on `:8443` when `CLAVIS_TLS` is set),
`CLAVIS_DATA_DIR=/data` and `CLAVIS_LOG_FORMAT=json`. PostgreSQL, SSO, backups and
the rest are configured with `CLAVIS_*` environment variables; see the
[configuration reference](https://dumhouse.github.io/clavis/quickstart).

## The CLI

The same binary is the CLI. Install it with the script, Homebrew, Scoop, or a
release archive; see https://dumhouse.github.io/clavis/cli. Every command, with
flags and examples, is in the [CLI reference](https://dumhouse.github.io/clavis/reference/).

Admin commands run inside the container, for example a password reset link when
no email is configured:

```sh
docker exec clavis clavis admin reset-password you@example.com
```
