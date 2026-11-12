# Reproducible builds

The release build is designed to be reproducible:

- `CGO_ENABLED=0`
- `-trimpath`
- no build timestamps in the binary except the explicit `version.Date` linker
  value
- pinned Go toolchain and module checksums
- pinned web dependencies through `pnpm-lock.yaml`

## Reproduce locally

```sh
git checkout v1.0.0
make web
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X github.com/dumhouse/clavis/internal/version.Version=1.0.0 -X github.com/dumhouse/clavis/internal/version.Commit=$(git rev-parse --short HEAD) -X github.com/dumhouse/clavis/internal/version.Date=2026-01-01T00:00:00Z" \
  -o clavis ./cmd/clavis
sha256sum clavis
```

Compare against the published `checksums.txt` for the same OS/arch. If the hash
differs, confirm the Go version, OS/arch, and `version.Date` value first.

## Caveats

- Go binaries are generally reproducible with the same toolchain and flags.
- The embedded SPA is built with Vite; Vite output is deterministic for a fixed
  lockfile and Node version.
- The release provenance records the exact workflow, commit, and builder.
