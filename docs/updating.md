---
description: "How Clavis tells you about new releases, how it works out the update command for your install (Homebrew, Scoop, WinGet, Docker, Helm, packages, source), and how to turn the check off."
---

# Updating Clavis

Clavis checks for new releases on its own and tells you the exact command to
update, based on how it was installed. The CLI and the server are the same
binary, but you update them separately: the CLI on each computer, the server
where it runs.

## Update notices

**In the web app.** Owners and admins see a notice at the top of the page
when a newer release exists. It shows the version this server runs, how it was
installed, the command that updates it (with a copy button), and a link to the
release notes. **Later** hides it for a week; a newer release shows it again.

**In the CLI.** After an interactive command, the CLI prints a short note
when an update is waiting, at most once every three days:

```text
! Clavis 0.3.3 is available (you have 0.3.2). Run: clavis upgrade
  Run `clavis update` for details. Turn these notes off with CLAVIS_UPDATE_CHECK=false.
```

It also warns when the CLI and your server differ in minor version (for
example a 0.3 CLI with a 0.4 server), since that is when commands may stop
working.

## `clavis update` and `clavis upgrade`

The two commands follow the usual split, as in `apt update` / `apt upgrade`:

- `clavis update` **checks**. It changes nothing.
- `clavis upgrade` **installs** the latest release.

```sh
clavis update
```

```text
This CLI:       0.3.2 (Homebrew)
Latest release: 0.3.3, update available
Server:         https://clavis.example.com runs 0.3.3

Upgrade with:
  clavis upgrade
  (runs: brew upgrade dumhouse/tap/clavis)
```

What `clavis upgrade` does depends on how Clavis was installed:

| Installed with | `clavis upgrade` |
| --- | --- |
| Homebrew, Scoop, WinGet, `go install` | Runs the package manager's command after you confirm (`--yes` skips the question). |
| Install script, or a binary you downloaded | Downloads the release for your system, checks it against the release's `checksums.txt` (and verifies that file's signature when [`cosign`](https://docs.sigstore.dev/cosign/system_config/installation/) is installed), and replaces itself. The old binary is kept as `clavis.old` until the next upgrade. |
| Docker, Helm, `.deb`, `.rpm`, `.apk`, source build | Prints the command to run. These are upgraded outside the CLI, often with `sudo`. |

Flags:

- `--yes`: no question, for scripts.
- `--version 0.3.3`: a specific release instead of the latest.
- `--download-base URL` (or `CLAVIS_DOWNLOAD_BASE`): download from a mirror
  that serves the release files under `URL/v0.3.3/`.

To verify an archive you downloaded yourself, pass it:
`clavis upgrade clavis_0.3.3_linux_amd64.tar.gz` checks it with cosign, and
`--install` also puts it in place.

## How the install is detected

Clavis looks at where its binary lives and what it runs in:

| Installed with | Detected by | Update command |
| --- | --- | --- |
| Homebrew | binary under Homebrew (`/opt/homebrew`, `Cellar`, `Caskroom`, Linuxbrew) | `brew upgrade dumhouse/tap/clavis` |
| Scoop | binary under `scoop\apps` | `scoop update clavis` |
| WinGet | binary under `WinGet\Packages` | `winget upgrade Dumhouse.Clavis` |
| Docker | running in a container | `docker compose pull && docker compose up -d` |
| Helm | running in a Kubernetes pod | `helm upgrade clavis oci://ghcr.io/dumhouse/charts/clavis --namespace clavis --reuse-values` |
| `.deb` package | `/usr/bin/clavis` on Debian or Ubuntu | `sudo apt update && sudo apt install --only-upgrade clavis` if you installed from the apt repository; otherwise download the new `.deb` and `sudo dpkg -i` it |
| `.rpm` package | `/usr/bin/clavis` on Fedora, RHEL or SUSE | `sudo dnf upgrade clavis` if you installed from the rpm repository; otherwise `sudo rpm -U <release .rpm URL>` |
| `.apk` package | `/usr/bin/clavis` on Alpine | download the new `.apk` and `sudo apk add` it |
| `go install` | binary in `$GOBIN` with a module version | `go install github.com/dumhouse/clavis/cmd/clavis@latest` |
| Built from source | a development version (`0.3.2+dev.<commit>`) | `git pull && make build` |
| Install script | `/usr/local/bin` or `~/.local/bin` | re-run the [install script](./download) |

There are no hosted apt, yum or apk repositories yet, so package installs
download each release from GitHub. A binary you put somewhere else is reported as a downloaded
binary, with the install script as the suggestion.

## What the check sends

One `GET` request to GitHub's public "latest release" API, at most once a day
from the server and once a day from each computer running the CLI. It sends
no information about your server, users or secrets. When GitHub can't be
reached, Clavis tries again an hour later and stays quiet.

## Turning it off

| Where | How |
| --- | --- |
| Server | `CLAVIS_UPDATE_CHECK=false` (or `server.update_check: false`) |
| Server behind a mirror | `CLAVIS_UPDATE_CHECK_URL=https://mirror.example.com/latest`, answering like GitHub's API |
| CLI | `CLAVIS_UPDATE_CHECK=false` (the same variable) |

The CLI never checks in CI (when `CI` is set), with `--json`, or when its
output isn't a terminal, and commands whose output other programs read
(`run`, `export`, `shell`, `mcp`) never print the note.

## Which version am I running?

The version is in the hint bar at the bottom of the web app and in
`clavis version`. A release shows as `v0.3.3`. A build from source shows as
`0.3.2 dev 14.11.26 14:23*`: the release it comes after, the build time, and a
`*` when it was built with uncommitted changes. Hover the label for the
commit.
