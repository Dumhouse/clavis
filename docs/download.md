---
description: "Download Clavis for macOS, Linux and Windows on x86-64 and ARM, or install it with Homebrew, Scoop, Docker or the install script."
---

# Download

Pick your operating system and CPU. The CLI and the server are the same binary.

<DownloadMatrix />

## Package managers and Docker

| Platform | Command |
| --- | --- |
| macOS | `brew install dumhouse/tap/clavis` |
| Windows | `scoop bucket add dumhouse https://github.com/Dumhouse/scoop-bucket && scoop install clavis` |
| Linux or macOS | `curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/main/scripts/install.sh \| bash` |
| Docker (server) | `docker run -d --name clavis --restart unless-stopped -p 80:8080 -v clavis-data:/data dumhouse/clavis:latest` (or use the [install wizard](./install)) |

More methods are listed in the [CLI docs](./cli#install-the-cli). To check a
download, see [Supply chain](./supply-chain).

## Updating

Clavis tells you when a new release is out and which command updates your
install. See [Updating Clavis](./updating), or run `clavis update`.
