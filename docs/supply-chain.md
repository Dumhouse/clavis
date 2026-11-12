# Supply chain

## Release artifacts

Releases are built only in GitHub Actions with GoReleaser. Each release
contains:

- static binaries for Linux, macOS, and Windows on amd64/arm64
- `checksums.txt`
- SPDX SBOMs
- cosign keyless signatures (`*.sigstore.json`)

The release workflow uses `id-token: write` for Sigstore keyless signing and
attaches the signature bundles to the GitHub release.

## Verify a release

```sh
cosign verify-blob \
  --bundle clavis_1.0.0_linux_amd64.tar.gz.sigstore.json \
  --certificate-identity-regexp 'https://github.com/dumhouse/clavis/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  clavis_1.0.0_linux_amd64.tar.gz
sha256sum -c checksums.txt
```

`clavis upgrade --file ... --bundle ...` performs the same verification through
the `cosign` binary and refuses to install an unverified artifact.

## SLSA provenance

The release workflow requests SLSA Build L3 provenance through the SLSA GitHub
Generator. Download `multiple.intoto.jsonl` from the release and verify it with
`slsa-verifier`:

```sh
slsa-verifier verify-artifact clavis_1.0.0_linux_amd64.tar.gz \
  --provenance-path multiple.intoto.jsonl \
  --source-uri github.com/dumhouse/clavis
```

## Images

Container images are multi-arch, distroless, non-root, and signed with cosign.
Verify the image digest, not the mutable tag:

```sh
cosign verify ghcr.io/dumhouse/clavis@sha256:...
```

## Dependency policy

- New direct dependencies require a justification in the PR.
- `go.mod`, `go.sum`, `pnpm-lock.yaml`, and `csi/go.sum` are committed.
- Renovate and Dependabot updates are reviewed like code.
- `govulncheck`, CodeQL, Trivy, and dependency review run in CI.
