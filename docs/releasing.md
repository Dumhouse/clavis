---
description: "How a Clavis release is built and published: the tag, the tokens each channel needs, and how to set up Homebrew, Scoop, Docker Hub, apt and rpm."
---

# Releasing and packaging

A release is one git tag. Pushing a `v*` tag runs the **Release** workflow,
which runs [GoReleaser](https://goreleaser.com). GoReleaser builds everything
and publishes it to each channel below.

| Channel | What is published | Needs |
| --- | --- | --- |
| GitHub Releases | Archives, checksums, SBOMs, cosign signatures, `.deb`, `.rpm`, `.apk` | Nothing extra |
| SLSA provenance | Provenance attestation for the archives | Nothing extra |
| `ghcr.io/dumhouse/clavis` | Multi-arch container image | Nothing extra |
| `dumhouse/clavis` on Docker Hub | The same image | `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN` |
| Homebrew tap | A cask in `Dumhouse/homebrew-tap` | `HOMEBREW_TAP_TOKEN` |
| Scoop bucket | A manifest in `Dumhouse/scoop-bucket` | `SCOOP_BUCKET_TOKEN` |
| winget | A pull request to `microsoft/winget-pkgs` | `WINGET_TOKEN` and a `Dumhouse/winget-pkgs` fork |
| apt and rpm repositories | Hosted `.deb` and `.rpm` on Cloudsmith | `CLOUDSMITH_API_KEY` |
| Helm chart | OCI chart at `ghcr.io/dumhouse/charts` | Nothing extra (separate workflow) |
| npm, PyPI, Dev Container Feature | The SDKs and the feature | See [below](#sdks-and-the-devcontainer-feature) (separate workflow) |

Channels whose secret is missing are skipped, so a release never fails because
a token has not been set up yet.

After the release job, a **Verify published release** job pulls and runs both
container images, checks that the release has the expected archives and
packages, and checks that the Homebrew cask and Scoop manifest list the new
version. A failure there fails the run.

## What is in a GitHub release

A release page lists about 27 files. Most are signatures and software bills of
materials for the few files you actually download:

| Files | Count | Purpose |
| --- | --- | --- |
| `clavis_<version>_<os>_<arch>.tar.gz` / `.zip` | 6 | The CLI and server, for Linux, macOS and Windows on amd64 and arm64 |
| `.deb`, `.rpm`, `.apk` | 6 | The same binary as Linux packages |
| `*.sbom.json` | 6 | A software bill of materials for each archive |
| `<archive>.sigstore.json` | 6 | A cosign signature for each archive |
| `checksums.txt` and its `.sigstore.json` | 2 | SHA-256 of every artifact, signed |
| `multiple.intoto.jsonl` | 1 | SLSA provenance for the archives |

Users normally need one archive or package. The rest exist so anyone can verify
what they downloaded, and the install script checks the archive's signature when
`cosign` is installed. See [Supply chain](./supply-chain).

Packages and SBOMs are not signed one by one. Verify them with the signed
`checksums.txt`: check its signature, then check the file's SHA-256 against it.

## Test the configuration locally

Install GoReleaser (`brew install goreleaser`), then:

```sh
export HOMEBREW_TAP_TOKEN= SCOOP_BUCKET_TOKEN= WINGET_TOKEN=
goreleaser check
goreleaser release --snapshot --clean --skip=publish,sign,docker,sbom
```

The empty variables are needed because the config reads each repository token
through GoReleaser's `.Env` template map, which must be written in exactly that
form and fails when the variable does not exist. The release workflow always
defines them.
`sbom` needs `syft` (`brew install syft`); drop it from `--skip` if you have it.

The second command builds everything into `dist/` and publishes nothing.
`goreleaser check` also prints deprecation warnings for options that still work.

## Create the tokens

### Homebrew and Scoop (`HOMEBREW_TAP_TOKEN`, `SCOOP_BUCKET_TOKEN`)

`GITHUB_TOKEN` inside Actions can only write to the repository that is running.
GoReleaser pushes into two other repositories, so it needs a token that can.

1. On GitHub: **Settings → Developer settings → Personal access tokens →
   Fine-grained tokens → Generate new token**.
2. **Resource owner:** the organization (`Dumhouse`). If it is not listed, an
   organization owner must allow fine-grained tokens in the organization's
   settings, and may have to approve the token afterwards.
3. **Repository access:** *Only select repositories*, then `homebrew-tap` and
   `scoop-bucket`.
4. **Repository permissions:** *Contents: Read and write*. Nothing else.
5. Set the longest expiry offered and add a reminder to renew it.
6. Copy the token (`github_pat_…`). It is shown once.
7. In the `clavis` repository, **Settings → Secrets and variables → Actions →
   New repository secret**. Create `HOMEBREW_TAP_TOKEN` and
   `SCOOP_BUCKET_TOKEN` with the same value.

### Docker Hub (`DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`)

1. On Docker Hub: **Account settings → Personal access tokens → Generate new
   token** with *Read & write* access and an expiry.
2. The account must be an owner of, or have write access to, the `dumhouse`
   namespace. The `dumhouse/clavis` repository is created on first push.
3. Add `DOCKERHUB_USERNAME` (your Docker Hub username) and `DOCKERHUB_TOKEN`
   as repository secrets.
4. Docker Hub only lets a token change a repository's description if the token
   has *Read, Write, Delete* access. With *Read & Write* the image still pushes,
   but the description step of the release fails. The description comes from
   `deploy/DOCKERHUB.md` and is updated on every release.

## Homebrew details

GoReleaser publishes a Homebrew **cask**, which it recommends over the older
formula generator it has deprecated. Users install with:

```sh
brew install dumhouse/tap/clavis
```

If the tap still holds an older `Formula/clavis.rb` from a previous release,
delete it once the cask is published. Having both makes `brew install clavis`
ambiguous.

## apt and rpm repositories

Releases attach `.deb`, `.rpm` and `.apk` files and also upload the `.deb` and
`.rpm` to a hosted, signed repository on **Cloudsmith**
(<https://cloudsmith.io/~dumhouse/repos/clavis/packages/>), which signs packages
for you. Users install with the setup scripts in [the CLI page](./cli).

1. Contact Cloudsmith about their open-source plan for public projects and wait
   for approval.
2. Create an organization and a repository (for example `dumhouse/clavis`). If
   yours has a different `owner/repository` name, set the repository variable
   `CLOUDSMITH_REPO` to it (Settings → Secrets and variables → Actions →
   Variables).
3. Create an API key and store it as the secret `CLOUDSMITH_API_KEY`.

The release workflow then uploads every `.deb` and `.rpm` with the Cloudsmith CLI
(`cloudsmith push deb` and `cloudsmith push rpm`). Without the secret that step is
skipped. Cloudsmith shows a one-line setup command for users on the repository's
page.

The repository must be public: in Cloudsmith, open the repository's **Broadcasts**
settings and set visibility to open source. Uploads succeed while it is private,
but the public URLs return 404.

## Action mirror

GitHub Marketplace lists actions that sit at the root of their own repository,
so each release copies `integrations/github-action` to `dumhouse/clavis-action`
and tags it with the release version (plus a moving major tag such as `v0`).
The workflow `publish-action.yml` does this and stays inert until you set it
up:

1. Create the empty repository `dumhouse/clavis-action`.
2. Add the secret `ACTION_MIRROR_TOKEN`, a token that can push to it.
3. Set the repository variable `PUBLISH_ACTION_MIRROR` to `true`.

The source stays in this repository, so changes to the action are reviewed
with the server they depend on. Run the workflow by hand with a tag to
publish an earlier release.

## SDKs and the devcontainer feature

The workflow `publish-packages.yml` runs when a release is published and has
one job per channel. Each job is skipped until you turn it on, and a job can
be run by hand with a tag to publish an earlier release.

| Channel | Package | Turn it on |
| --- | --- | --- |
| npm | `@dumhouse/clavis-client` (from `sdk/node`) | Secret `NPM_TOKEN`, variable `PUBLISH_NPM=true` |
| PyPI | `clavis-client` (from `sdk/python`) | Trusted publisher, variable `PUBLISH_PYPI=true` |
| Dev Container Feature | `ghcr.io/dumhouse/clavis/clavis` | Variable `PUBLISH_DEVCONTAINER_FEATURE=true` |

**npm.** Create the `@dumhouse` organization on npmjs.com, then a granular
access token that can publish to it, and store it as `NPM_TOKEN`. Packages
are published with provenance, so npm shows which workflow built them. The
package version is the release tag without the `v`.

**PyPI.** No token is needed. On pypi.org, add a pending trusted publisher for
the project `clavis-client`: owner `Dumhouse`, repository `clavis`, workflow
`publish-packages.yml`, environment `pypi`. Create the `pypi` environment in
the repository settings (you can require a reviewer on it).

**Dev Container Feature.** It is published to GitHub Container Registry with
the workflow's own token. After the first publish, make the package public in
the organization's package settings. The feature has its own version in
`devcontainer/features/clavis/devcontainer-feature.json`: bump it when
`install.sh` or the options change. The CLI version it installs is chosen by
the user with the `version` option.

## winget

winget manifests are submitted as a pull request to
[microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs). GoReleaser
generates the three manifest files (a portable zip installer for amd64 and arm64)
and opens that pull request from a fork owned by the organization.

One-time setup:

1. Fork `microsoft/winget-pkgs` into the organization, so the fork is
   `Dumhouse/winget-pkgs`. Keep the name.
2. Create a **classic** personal access token with the `public_repo` scope,
   from an account that can push to the fork (details below).
3. Store it as the secret `WINGET_TOKEN`.
4. Turn the step on by creating the repository **variable** `WINGET_ENABLED` with
   the value `true` (Settings → Secrets and variables → Actions → Variables).
   Until then the step is skipped, so a problem with the token cannot fail a
   release.

The token must be able to open a pull request on `microsoft/winget-pkgs`, a
repository the organization does not own. A fine-grained token cannot do that,
because its resource owner can only be the organization or your account. Use a
**classic** token with the `public_repo` scope, created by an account that owns or
can push to the fork.

What to expect:

- The first submission of a new package is reviewed by Microsoft's moderators and
  an automated validation pipeline, and can take days. Later versions are
  usually merged automatically once validation passes.
- Fix anything the validation bot reports on the pull request, then push to the
  same branch (`clavis-<version>` on the fork).
- Once it is accepted, users install with `winget install Dumhouse.Clavis`.
- Keep the fork's default branch in sync with upstream (the GitHub "Sync fork"
  button), or GoReleaser opens pull requests based on an outdated branch.

## Troubleshooting

- **A channel did not publish:** open the Release run and look at the
  `goreleaser` step. Missing-secret skips are logged.
- **Token expired:** create a new one and update the secret. Releases keep
  working; only that channel is skipped or fails.
- **Tag pushed from the wrong branch:** delete the tag with
  `git push origin :refs/tags/vX.Y.Z`, delete any draft or published release,
  and tag `main` again.
