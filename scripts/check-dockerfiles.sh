#!/bin/sh
# deploy/Dockerfile (built from source) and deploy/Dockerfile.goreleaser (used
# for releases) must produce the same runtime image. Compare their final stages,
# ignoring the line that copies the binary in, which differs on purpose.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)

runtime_stage() {
  awk '/^FROM gcr\.io\/distroless\//{on=1} on' "$1" |
    grep -v '^COPY .*clavis /usr/local/bin/clavis$' |
    grep -v '^ARG TARGETPLATFORM$'
}

a=$(mktemp)
b=$(mktemp)
trap 'rm -f "$a" "$b"' EXIT
runtime_stage "$root/deploy/Dockerfile" >"$a"
runtime_stage "$root/deploy/Dockerfile.goreleaser" >"$b"
[ -s "$a" ] || { echo "check-dockerfiles: no runtime stage in deploy/Dockerfile" >&2; exit 1; }
if ! diff -u "$a" "$b"; then
  echo "check-dockerfiles: the runtime stages differ; keep them in sync" >&2
  exit 1
fi
echo "check-dockerfiles: ok"
