#!/bin/sh
# Update a Docker Compose install of Clavis to a release, with a backup first
# and an automatic rollback if the new version does not become healthy.
#
#   ./update.sh 0.3.5
#
# Run it next to compose.yaml and .env. It:
#   1. stops Clavis and copies the data volume to ./backups (a consistent copy),
#   2. sets CLAVIS_VERSION in .env and pulls the image,
#   3. starts it and waits for the image's health check,
#   4. on failure, restores the old version and starts that again.
set -eu

cd "$(dirname "$0")"

die() { echo "update: $*" >&2; exit 1; }

[ $# -eq 1 ] || die "usage: ./update.sh VERSION   (for example 0.3.5, without a leading v)"
new="${1#v}"
case "$new" in
  *[!0-9A-Za-z._-]* | "") die "'$1' is not a version" ;;
esac
[ -f .env ] || die ".env not found; copy .env.example to .env first"
command -v docker >/dev/null 2>&1 || die "docker is not installed"
docker compose version >/dev/null 2>&1 || die "docker compose is not available"

old=$(sed -n 's/^CLAVIS_VERSION=//p' .env | tail -n 1)
[ -n "$old" ] || die "CLAVIS_VERSION is not set in .env"
[ "$old" != "$new" ] || die "already on $new"

set_version() {
  tmp=$(mktemp ./.env.XXXXXX)
  awk -v v="$1" 'BEGIN{done=0} /^CLAVIS_VERSION=/{print "CLAVIS_VERSION=" v; done=1; next} {print} END{if(!done) print "CLAVIS_VERSION=" v}' .env >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" .env
}

echo "Updating Clavis $old -> $new"

# Pull first, so a typo in the version fails before anything is stopped.
set_version "$new"
# A pinned version is pulled when it is not on this machine yet; a floating tag
# such as latest is always checked.
policy=missing
[ "$new" != latest ] || policy=always
if ! docker compose pull --policy "$policy"; then
  set_version "$old"
  die "could not pull version $new; nothing was changed"
fi

# A stopped database is the only way to copy SQLite safely. This is a short stop.
volume=$(docker volume ls -q --filter label=com.docker.compose.volume=clavis-data | head -n 1)
[ -n "$volume" ] || die "could not find the clavis-data volume"
mkdir -p backups
chmod 700 backups
stamp=$(date +%Y%m%d-%H%M%S)
file="clavis-data-$old-$stamp.tgz"
docker compose stop
if ! docker run --rm -v "$volume":/data:ro -v "$PWD/backups":/backups alpine \
  tar czf "/backups/$file" -C /data .; then
  set_version "$old"
  docker compose up -d --wait || true
  die "backup failed; restarted $old and changed nothing"
fi
echo "Backup: backups/$file"

if docker compose up -d --wait; then
  echo "Clavis $new is healthy."
  docker compose exec -T clavis clavis version
  exit 0
fi

echo "update: $new did not become healthy. Rolling back to $old." >&2
docker compose logs --tail 30 clavis >&2 || true
set_version "$old"
docker compose up -d --wait || die "rollback to $old also failed; data backup is backups/$file"
die "rolled back to $old; $new was not applied"
