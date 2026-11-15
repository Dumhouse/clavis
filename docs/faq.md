---
description: "Quick answers to common Clavis problems: masked values, login and server errors, certificates, updates, CI, and where to look when something doesn't work."
---

# FAQ

**Something doesn't work. Where do I start?**
Run `clavis doctor`. It checks the CLI, `.clavis.yaml`, the server address,
HTTPS trust, versions, your clock, your sign-in and your project access, and
prints the command that fixes each problem. `clavis help` finds any command.

**`clavis run rails c` shows `"••••••"` instead of the value.**
That is masking. Your code still gets the real value; only printed output is
hidden. To see it on your own screen, run `clavis run --no-mask rails c`. See
[Consoles and masking](./guides/consoles-and-masking).

**`clavis run rails c` shows no prompt, or looks frozen.**
Older versions piped the program's output, and Ruby draws its prompt only on a
terminal. Upgrade the CLI (`clavis upgrade`). If a program still misbehaves,
try `clavis run --no-pty COMMAND`.

**Error: no project context: run `clavis init`.**
The folder isn't linked to a project. Run `clavis init` in it, or pass
`--org`, `--project` and `--env`.

**Error: not logged in to http://localhost:8080.**
The CLI uses `localhost:8080` when it doesn't know your server. Sign in with
`clavis login --server https://your-server`. It remembers the server for the
next commands, and `.clavis.yaml` can store it for a project.

**The CLI says the server's certificate isn't trusted.**
A self-signed server needs to be trusted once on each computer:
`clavis trust add https://your-server`. It shows the fingerprint to compare
with the one the server printed at start-up.

**How do I update the CLI, and the server?**
`clavis update` checks and `clavis upgrade` installs, the way Clavis was
installed. The server is updated where it runs; see [Updating Clavis](./updating).

**How do I turn off the update notices?**
`CLAVIS_UPDATE_CHECK=false` for both the CLI and the server.

**Can I use Clavis in CI?**
Yes. Create a machine token and give it to the job as `CLAVIS_TOKEN`. See
[CI and machine tokens](./ci), or the guides for [GitHub Actions](./guides/github-actions-secrets),
[Forgejo and Gitea](./guides/forgejo-actions-secrets) and [GitLab CI](./guides/gitlab-ci-secrets).

**Do my other commands see the secrets?**
Only the command you start with `clavis run` (and what it starts). Your shell
and other terminals don't get them. For a shell with the secrets loaded, use
`clavis shell`.

**How do I stop secrets from being committed to git?**
`clavis scan --install-hook` blocks commits that contain Clavis tokens; see
[Catch leaked secrets before commit](./guides/pre-commit).

**Where can I ask a question or report a problem?**
In the web app, open the **?** menu: it links to Discussions and Issues and
has **Copy diagnostics**, which gathers versions and your browser for the
report. In a terminal, `clavis doctor --json` does the same for the CLI.
