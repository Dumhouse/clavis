#!/bin/sh
# Build deterministic release-shaped binaries without publishing anything.
set -eu
VERSION=${VERSION:-0.0.0-dryrun}
COMMIT=$(git rev-parse --short HEAD)
DATE=${SOURCE_DATE_EPOCH:-0}
OUT=dist/dry-run
rm -rf "$OUT"
mkdir -p "$OUT"
make web >/dev/null
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  GOOS=${target%/*}
  GOARCH=${target#*/}
  name="clavis_${VERSION}_${GOOS}_${GOARCH}"
  [ "$GOOS" = windows ] && name="$name.exe"
  echo "building $name"
  CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath \
    -ldflags "-s -w -X github.com/dumhouse/clavis/internal/version.Version=$VERSION -X github.com/dumhouse/clavis/internal/version.Commit=$COMMIT -X github.com/dumhouse/clavis/internal/version.Date=$DATE" \
    -o "$OUT/$name" ./cmd/clavis
  go version -m "$OUT/$name" > "$OUT/$name.buildinfo"
done
cd "$OUT"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum clavis_* > checksums.txt
else
  shasum -a 256 clavis_* > checksums.txt
fi
echo "wrote $OUT/checksums.txt (not signed; signing is CI-only)"
