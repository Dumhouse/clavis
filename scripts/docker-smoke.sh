#!/bin/sh
# Build the release image from a clavis binary and prove the Docker setup in
# deploy/ works: it becomes healthy, update.sh updates it, and update.sh rolls
# back when the new version is broken. Needs docker and docker compose.
#
#   scripts/docker-smoke.sh bin/clavis      (a linux/amd64 binary)
set -eu

bin=${1:?usage: scripts/docker-smoke.sh path/to/clavis-linux-amd64}
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'cd "$root"; (cd "$work/deploy" && docker compose down -v >/dev/null 2>&1) || true' EXIT

fail() { echo "docker-smoke: $*" >&2; exit 1; }

# The image, built from the same Dockerfile GoReleaser uses.
ctx="$work/ctx"
mkdir -p "$ctx/linux/amd64"
cp "$bin" "$ctx/linux/amd64/clavis"
chmod +x "$ctx/linux/amd64/clavis"
build() {
  docker build -q -f "$root/deploy/Dockerfile.goreleaser" \
    --build-arg TARGETPLATFORM=linux/amd64 -t "ghcr.io/dumhouse/clavis:$1" "$ctx" >/dev/null
}
build one
build two

# A "release" that starts and then exits, to prove the rollback.
cat >"$work/Dockerfile.broken" <<'DOCKER'
FROM ghcr.io/dumhouse/clavis:two
CMD ["version"]
DOCKER
docker build -q -f "$work/Dockerfile.broken" -t ghcr.io/dumhouse/clavis:broken "$work" >/dev/null

# Use the files a user would copy.
mkdir -p "$work/deploy"
cp "$root/deploy/compose.yaml" "$root/deploy/.env.example" "$root/deploy/update.sh" "$work/deploy/"
cd "$work/deploy"
cp .env.example .env
sed -i 's/^CLAVIS_VERSION=.*/CLAVIS_VERSION=one/' .env
sed -i 's/^CLAVIS_TLS=.*/CLAVIS_TLS=self-signed/' .env
sed -i 's#^CLAVIS_SERVER_BASE_URL=.*#CLAVIS_SERVER_BASE_URL=https://localhost:18443#' .env
sed -i 's/^CLAVIS_HTTP_PORT=.*/CLAVIS_HTTP_PORT=18080/; s/^CLAVIS_HTTPS_PORT=.*/CLAVIS_HTTPS_PORT=18443/' .env
export COMPOSE_PROJECT_NAME=clavis_smoke

echo "== start: the image becomes healthy (read-only filesystem, TLS on)"
docker compose up -d --wait --wait-timeout 90 || { docker compose logs clavis; fail "never became healthy"; }
[ "$(docker inspect -f '{{.State.Health.Status}}' clavis_smoke-clavis-1)" = healthy ] || fail "not healthy"
docker compose exec -T clavis clavis healthcheck || fail "healthcheck command failed"

echo "== update.sh to a good version"
./update.sh two
ls backups/clavis-data-one-*.tgz >/dev/null || fail "no backup written"
grep -q '^CLAVIS_VERSION=two$' .env || fail "version not updated"

echo "== update.sh to a broken version rolls back"
if ./update.sh broken; then fail "a broken version was reported as a success"; fi
grep -q '^CLAVIS_VERSION=two$' .env || fail "version not rolled back"
docker compose up -d --wait --wait-timeout 90 || fail "old version not running after rollback"
echo "docker-smoke: ok"
