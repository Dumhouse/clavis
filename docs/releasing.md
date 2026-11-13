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

Channels whose secret is missing are skipped, so a release never fails because
a token has not been set up yet.

After the release job, a **Verify published release** job pulls and runs both
container images, checks that the release has the expected archives and
packages, and checks that the Homebrew cask and Scoop manifest list the new
version. A failure there fails the run.

## What is in a GitHub release

A release page lists about 40 files. Most are signatures and software bills of
materials for the few files you actually download:

| Files | Count | Purpose |
| --- | --- | --- |
| `clavis_<version>_<os>_<arch>.tar.gz` / `.zip` | 6 | The CLI and server, for Linux, macOS and Windows on amd64 and arm64 |
| `.deb`, `.rpm`, `.apk` | 6 | The same binary as Linux packages |
| `*.sbom.json` | 6 | A software bill of materials for each archive |
| `*.sigstore.json` | 24 | A cosign signature for every archive, package and SBOM |
| `checksums.txt` and its `.sigstore.json` | 2 | SHA-256 of every artifact, signed |
| `multiple.intoto.jsonl` | 1 | SLSA provenance for the archives |

Users normally need one archive or package. The rest exist so anyone can verify
what they downloaded, and the install script checks the archive's signature when
`cosign` is installed. See [Supply chain](./supply-chain).

If the list is too long, the usual cut is to sign only `checksums.txt` (set
`signs.artifacts: checksum` in `.goreleaser.yaml`), which removes about 18
signature files. That also removes the per-archive signature the install script
and the supply-chain docs rely on, so those would need updating first.

## Cut a release

1. Add the version's notes to `CHANGELOG.md`.
2. Make sure `main` has the commit you want and its checks are green.
3. Tag `main` and push the tag. Use your GitHub no-reply address for the tagger,
   or GitHub rejects the push (error GH007, "would publish a private email
   address"):

   ```sh
   git switch main && git pull
   GIT_COMMITTER_EMAIL=ID+USERNAME@users.noreply.github.com \
     git -c user.email=ID+USERNAME@users.noreply.github.com \
     tag -a v0.2.2 -m "Clavis v0.2.2"
   git push origin v0.2.2
   ```

4. Watch the **Release** run on the Actions tab. When it finishes, check the
   release page, `docker pull`, and the tap and bucket repositories.

Tag from `main` only. A tag on an unmerged branch publishes a release whose
commit is not in the project history.

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

Releases already attach `.deb`, `.rpm` and `.apk` files. For
`apt install clavis` to work they must also live in a hosted, signed
repository. Running one yourself means GPG keys and a web server. A hosted
service is simpler; this project plans to use **Cloudsmith**, which offers
open-source hosting and signs packages for you.

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

Until the repository is set up, install the packages from the release page.

## winget

winget manifests are submitted as a pull request to
[microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs). GoReleaser
generates the three manifest files (a portable zip installer for amd64 and arm64)
and opens that pull request from a fork owned by the organization.

One-time setup:

1. Fork `microsoft/winget-pkgs` into the organization, so the fork is
   `Dumhouse/winget-pkgs`. Keep the name.
2. Create a fine-grained token with the organization as owner, only the
   `winget-pkgs` fork selected, and *Contents: Read and write* plus
   *Pull requests: Read and write*. If the organization restricts tokens, the
   pull request itself is created against `microsoft/winget-pkgs`, so a classic
   token with `public_repo` scope is the fallback.
3. Store it as the secret `WINGET_TOKEN`. Until it exists the step is skipped.

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
