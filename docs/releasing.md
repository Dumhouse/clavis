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
| Helm chart | OCI chart at `ghcr.io/dumhouse/charts` | Nothing extra (separate workflow) |

Channels whose secret is missing are skipped, so a release never fails because
a token has not been set up yet.

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
goreleaser check
goreleaser release --snapshot --clean --skip=publish,sign,docker
```

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
2. Create an organization and a repository (for example `dumhouse/clavis`).
3. Create an API key and store it as the secret `CLOUDSMITH_API_KEY`.
4. Add a release step that uploads each package with the Cloudsmith CLI
   (`cloudsmith push deb` / `cloudsmith push rpm`). Cloudsmith then shows a
   one-line setup command for users.

Until this is done, install the packages from the release page.

## winget

winget manifests are submitted as a pull request to
[microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs).

1. Fork `microsoft/winget-pkgs` into the organization.
2. Create a token with *Contents* and *Pull requests* write access on the fork
   and store it as `WINGET_TOKEN`.
3. Add GoReleaser's `winget` section to `.goreleaser.yaml`.

Do this once there is a Windows build you are ready to support.

## Troubleshooting

- **A channel did not publish:** open the Release run and look at the
  `goreleaser` step. Missing-secret skips are logged.
- **Token expired:** create a new one and update the secret. Releases keep
  working; only that channel is skipped or fails.
- **Tag pushed from the wrong branch:** delete the tag with
  `git push origin :refs/tags/vX.Y.Z`, delete any draft or published release,
  and tag `main` again.
