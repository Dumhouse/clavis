---
description: "Save a Forgejo, GitHub, GitLab, or cloud credential once per organization and reuse it across Clavis syncs; rotate it in one place."
---

# Platform tokens for syncs

::: tip Push or fetch?
Clavis gets secrets to other systems in two ways:

- **Sync (push):** Clavis writes values into GitHub, Forgejo, Vercel, AWS and
  others, using *that platform's* access token. This page is about those
  tokens, shown as **Platform tokens** in the app (`clavis connections` in
  the CLI).
- **CI access (fetch):** a pipeline asks Clavis for one environment when it
  runs, with a *Clavis* machine token. No platform token is involved. See
  [Forgejo and Gitea Actions](./forgejo-actions-secrets) and
  [GitHub Actions](./github-actions-secrets).

In the app, **Integrations → Set up an integration** asks which platform you
use and suggests one of the two, with the reasons.
:::

A **platform token** (a *connection* in the API and CLI) is a provider credential saved once for the organization: a
Forgejo, GitHub, or GitLab token, a GitHub App, Vercel, Netlify, Cloudflare or
Vault tokens, AWS access keys, or a GCP service account, plus the instance URL.
Syncs pick a connection instead of carrying their own token, so the second sync
to the same Forgejo server only asks for the repository and environment.

- The credential is encrypted with the organization key (bound to the
  connection ID) and is never returned by the API, CLI, or UI.
- Rotating a connection updates every sync that uses it on its next run.
- A connection cannot be deleted while syncs use it; the error lists them.
- Plain HTTP syncs keep their own signing secret: it belongs to one receiver.

## In the web UI

**Integrations → Platform tokens → Add connection.** Use **Test** to make one cheap
authenticated call (for Forgejo, `GET /api/v1/user`; for AWS, STS
`GetCallerIdentity`) and see a clear error if the token is rejected or the
server is unreachable.

When you add a sync, step 2 lists the platform tokens for that provider. Pick
one, or choose **Enter an access token**: it is saved for other syncs unless you
untick **Save it for other syncs**.

For Forgejo, Gitea, GitHub and GitLab, step 3 then lists the repositories (and
organizations) the token can reach, so you pick instead of typing `owner/name`.
Repositories where the token lacks admin rights are flagged, since writing
Actions secrets needs them. The list is fetched by the Clavis server, so a
Forgejo on your LAN needs `CLAVIS_OUTBOUND_ALLOWLIST`.

## From the CLI

```sh
clavis connections create "Home Forgejo" --provider forgejo \
  --url https://git.example.com --token -          # reads the token from stdin
clavis connections test <ID>
clavis syncs create dev --provider forgejo --forgejo-repo team/web --connection <ID>
clavis connections rotate <ID> --token -
clavis connections ls
```

`clavis syncs create --forgejo-token …` (and the other token flags) still work
and store the credential on that sync only.

## Moving existing syncs onto a connection

Syncs created before connections keep working with their own token. To share
it, click **own token · share** on the sync, or:

```sh
clavis syncs use-connection <SYNC_ID> --name "Home Forgejo"   # new connection from this sync's token
clavis syncs use-connection <SYNC_ID> --connection <ID>      # use an existing connection
```

Either way the sync's own copy of the credential is dropped.

## Permissions and audit

| Action                                   | Needs                                          |
| ---------------------------------------- | ---------------------------------------------- |
| List connections (names, providers, URL) | `org:read`                                     |
| See which syncs use each connection      | `syncs:manage` org-wide                        |
| Create, rotate, rename, test, delete     | `syncs:manage` org-wide                        |
| Use a connection in a project's sync     | `syncs:manage` on that project                 |

Audit events: `sync_connection.create`, `sync_connection.update`,
`sync_connection.rotate`, `sync_connection.delete`, and `sync.connection` when
a sync is moved onto a connection.

API: `GET|POST /api/v1/orgs/{org}/connections`,
`PATCH|DELETE /api/v1/orgs/{org}/connections/{id}`,
`POST /api/v1/orgs/{org}/connections/{id}/test`, and
`PUT /api/v1/orgs/{org}/projects/{project}/syncs/{id}/connection`. Syncs accept
`connection_id` on create.

## Syncing to a server on your network

Clavis refuses to call private, loopback and link-local addresses, so a
connection or sync to a Forgejo, GitLab or Vault on your LAN fails with
`destination address is blocked`. This protects the server from being pointed
at your internal network. To allow specific destinations, list them in the
server's configuration:

```sh
CLAVIS_OUTBOUND_ALLOWLIST=192.168.100.0/24,10.0.0.5
```

The same setting in `clavis.yaml` is `security.outbound_allowlist`; in the Helm
chart it is `security.outboundAllowlist`. Entries are IPs or CIDRs, everything
not listed stays blocked, and the list also applies to webhooks, SSO discovery
and audit exporters. Restart the server after changing it.
