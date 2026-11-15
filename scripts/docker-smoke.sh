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
# sed -i differs between GNU and BSD, so edit through a temporary file.
edit_env() { sed "$1" .env >.env.tmp && mv .env.tmp .env; }
edit_env 's/^CLAVIS_VERSION=.*/CLAVIS_VERSION=one/'
edit_env 's/^CLAVIS_TLS=.*/CLAVIS_TLS=self-signed/'
edit_env 's#^CLAVIS_SERVER_BASE_URL=.*#CLAVIS_SERVER_BASE_URL=https://localhost:18443#'
edit_env 's/^CLAVIS_HTTP_PORT=.*/CLAVIS_HTTP_PORT=18080/; s/^CLAVIS_HTTPS_PORT=.*/CLAVIS_HTTPS_PORT=18443/'
export COMPOSE_PROJECT_NAME=clavis_smoke
# The root key in the data volume, to prove updates and rollbacks keep it.
root_key_sum() {
  docker run --rm -v clavis_smoke_clavis-data:/data:ro alpine:3 sha256sum /data/root.key | cut -d' ' -f1
}

echo "== start: the image becomes healthy (read-only filesystem, TLS on)"
docker compose up -d --wait --wait-timeout 90 || { docker compose logs clavis; fail "never became healthy"; }
[ "$(docker inspect -f '{{.State.Health.Status}}' clavis_smoke-clavis-1)" = healthy ] || fail "not healthy"
docker compose exec -T clavis clavis healthcheck || fail "healthcheck command failed"
key=$(root_key_sum)
[ -n "$key" ] || fail "no root key in the data volume"

echo "== update.sh to a good version"
./update.sh two
# The backup is root-owned and mode 600, so list it from a container.
backup=$(cd backups && ls clavis-data-one-*.tgz) || fail "no backup written"
listing=$(docker run --rm -v "$PWD/backups":/backups:ro alpine:3 tar tzf "/backups/$backup") || fail "cannot read the backup"
echo "$listing" | grep -q 'clavis.db' || fail "backup has no database"
if echo "$listing" | grep -q 'root.key'; then fail "backup contains the root key"; fi
[ "$(root_key_sum)" = "$key" ] || fail "root key changed during the update"
grep -q '^CLAVIS_VERSION=two$' .env || fail "version not updated"

echo "== update.sh to a broken version rolls back"
if ./update.sh broken; then fail "a broken version was reported as a success"; fi
grep -q '^CLAVIS_VERSION=two$' .env || fail "version not rolled back"
docker compose up -d --wait --wait-timeout 90 || fail "old version not running after rollback"
[ "$(root_key_sum)" = "$key" ] || fail "root key changed during the rollback"

echo "== a missing root key stops the server instead of replacing the key"
# The guard protects a database that holds data, so run first-run setup.
token=$(docker compose exec -T clavis clavis setup-token | tr -d '\r\n ')
[ -n "$token" ] || fail "no setup token"
curl -fsk -X POST "https://localhost:18443/api/v1/setup" -H 'Content-Type: application/json' \
  -d "{\"token\":\"$token\",\"email\":\"smoke@example.com\",\"name\":\"Smoke\",\"password\":\"correct-horse-battery\",\"org_slug\":\"acme\",\"org_name\":\"Acme\",\"project_slug\":\"web\",\"project_name\":\"Web\"}" >/dev/null \
  || fail "first-run setup failed"
docker compose stop
vol_sh() { docker run --rm -v clavis_smoke_clavis-data:/data alpine:3 sh -c "$1"; }
vol_sh 'mv /data/root.key /data/root.key.aside'
if docker compose up -d --wait --wait-timeout 30 >/dev/null 2>&1; then
  fail "started with a missing root key"
fi
docker compose logs clavis 2>&1 | grep -q 'root key .* is missing' || fail "no clear missing-key error in the logs"
[ -z "$(vol_sh 'ls /data/root.key 2>/dev/null')" ] || fail "a new root key was created"
docker compose down >/dev/null 2>&1 || true
vol_sh 'mv /data/root.key.aside /data/root.key'
docker compose up -d --wait --wait-timeout 90 || fail "did not start again with the original key"
[ "$(root_key_sum)" = "$key" ] || fail "root key changed after the missing-key test"
echo "docker-smoke: ok"
