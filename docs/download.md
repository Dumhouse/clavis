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
| Docker (server) | `docker run -p 8080:8080 -v clavis-data:/data dumhouse/clavis:latest` |

More methods are listed in the [CLI docs](./cli#install-the-cli). To check a
download, see [Supply chain](./supply-chain).
