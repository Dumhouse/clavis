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
docker run --rm -p 8080:8080 -v clavis-data:/data dumhouse/clavis:latest
```

Open http://localhost:8080. On first start Clavis writes a one-time setup token to
`/data/setup-token` inside the volume; paste it to create the owner account.

The volume holds the SQLite database **and the root key**. Back up the root key
separately from database backups: without it the secrets cannot be decrypted.

## Docker Compose

```yaml
services:
  clavis:
    image: dumhouse/clavis:latest
    ports:
      - "8080:8080"
    environment:
      CLAVIS_SERVER_BASE_URL: https://clavis.example.com
      CLAVIS_LOG_FORMAT: json
    volumes:
      - clavis-data:/data
    restart: unless-stopped

volumes:
  clavis-data:
```

## Tags

| Tag | Meaning |
| --- | --- |
| `latest` | The most recent release |
| `X.Y.Z` | A specific release, for example `0.2.2` |

Images are multi-arch (`linux/amd64`, `linux/arm64`), distroless, and run as a
non-root user. The same images are published to `ghcr.io/dumhouse/clavis`.

## Configuration

The image runs `clavis server` with `CLAVIS_SERVER_ADDR=:8080`,
`CLAVIS_DATA_DIR=/data` and `CLAVIS_LOG_FORMAT=json`. PostgreSQL, SSO, backups and
the rest are configured with `CLAVIS_*` environment variables; see the
[configuration reference](https://dumhouse.github.io/clavis/quickstart).

## The CLI

The same binary is the CLI. Install it with the script, Homebrew, Scoop, or a
release archive; see https://dumhouse.github.io/clavis/cli.
