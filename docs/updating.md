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
! Clavis 0.3.3 is available (you have 0.3.2). Update: brew upgrade dumhouse/tap/clavis
  Run `clavis update` for details. Turn these notes off with CLAVIS_NO_UPDATE_CHECK=1.
```

It also warns when the CLI and your server differ in minor version (for
example a 0.3 CLI with a 0.4 server), since that is when commands may stop
working.

`clavis update` checks right away and prints everything:

```sh
clavis update
```

```text
This CLI:       0.3.2 (Homebrew)
Latest release: 0.3.3, update available
Server:         https://clavis.example.com runs 0.3.3

Update with:
  brew upgrade dumhouse/tap/clavis
```

## How the install is detected

Clavis looks at where its binary lives and what it runs in:

| Installed with | Detected by | Update command |
| --- | --- | --- |
| Homebrew | binary under Homebrew (`/opt/homebrew`, `Cellar`, `Caskroom`, Linuxbrew) | `brew upgrade dumhouse/tap/clavis` |
| Scoop | binary under `scoop\apps` | `scoop update clavis` |
| WinGet | binary under `WinGet\Packages` | `winget upgrade Dumhouse.Clavis` |
| Docker | running in a container | `docker compose pull && docker compose up -d` |
| Helm | running in a Kubernetes pod | `helm upgrade clavis oci://ghcr.io/dumhouse/charts/clavis --namespace clavis --reuse-values` |
| `.deb` package | `/usr/bin/clavis` on Debian or Ubuntu | download the new `.deb` and `sudo dpkg -i` it |
| `.rpm` package | `/usr/bin/clavis` on Fedora, RHEL or SUSE | `sudo rpm -U <release .rpm URL>` |
| `.apk` package | `/usr/bin/clavis` on Alpine | download the new `.apk` and `sudo apk add` it |
| `go install` | binary in `$GOBIN` with a module version | `go install github.com/dumhouse/clavis/cmd/clavis@latest` |
| Built from source | a development version (`0.3.2+dev.<commit>`) | `git pull && make build` |
| Install script | `/usr/local/bin` or `~/.local/bin` | re-run the [install script](./download) |

There are no apt, yum or apk repositories yet, so package installs download
each release. A binary you put somewhere else is reported as a downloaded
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
| CLI | `CLAVIS_NO_UPDATE_CHECK=1` |

The CLI never checks in CI (when `CI` is set), with `--json`, or when its
output isn't a terminal, and commands whose output other programs read
(`run`, `export`, `shell`, `mcp`) never print the note.

## Which version am I running?

The version is in the hint bar at the bottom of the web app and in
`clavis version`. A release shows as `v0.3.3`. A build from source shows as
`0.3.2 dev 14.11.26 14:23*`: the release it comes after, the build time, and a
`*` when it was built with uncommitted changes. Hover the label for the
commit.
