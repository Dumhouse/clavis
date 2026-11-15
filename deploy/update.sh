#!/bin/sh
# Update a Docker Compose install of Clavis to a release, with a backup first
# and an automatic rollback if the new version does not become healthy.
#
#   ./update.sh 0.3.5
#
# Run it next to compose.yaml and .env. It:
#   1. stops Clavis and copies the data volume to ./backups (a consistent copy,
#      without the root key),
#   2. sets CLAVIS_VERSION in .env and pulls the image,
#   3. starts it and waits for the image's health check,
#   4. on failure, restores the old version and its data and starts that again.
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
# Find the data volume through this project's container, so another Clavis
# project on the same machine (the demo, say) is never touched.
helper=alpine:3
cid=$(docker compose ps -aq clavis | head -n 1)
[ -n "$cid" ] || die "no clavis container in this project; start it once with docker compose up -d"
volume=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' "$cid")
[ -n "$volume" ] || die "could not find the volume mounted at /data"
mkdir -p backups
chmod 700 backups
stamp=$(date +%Y%m%d-%H%M%S)
file="clavis-data-$old-$stamp.tgz"
docker compose stop
# The root key stays out of the copy: a file holding both the database and the
# key that decrypts it is the one thing an attacker needs. It stays in the
# volume, and a restore below keeps it there.
if ! docker run --rm -v "$volume":/data:ro -v "$PWD/backups":/backups "$helper" \
  tar czf "/backups/$file" --exclude './root.key*' -C /data .; then
  set_version "$old"
  docker compose up -d --wait || true
  die "backup failed; restarted $old and changed nothing"
fi
chmod 600 "backups/$file"
echo "Backup: backups/$file (without the root key)"

if docker compose up -d --wait; then
  echo "Clavis $new is healthy."
  docker compose exec -T clavis clavis version
  exit 0
fi

echo "update: $new did not become healthy. Rolling back to $old." >&2
docker compose logs --tail 30 clavis >&2 || true
set_version "$old"
# The new version may have migrated the database before it failed, and the old
# one cannot read a newer schema, so put the data back as it was.
docker compose stop
if ! docker run --rm -v "$volume":/data -v "$PWD/backups":/backups:ro "$helper" sh -c '
  set -e
  owner=$(stat -c %u:%g /data)
  find /data -mindepth 1 -maxdepth 1 ! -name "root.key*" -exec rm -rf {} +
  tar xzf "/backups/$1" -C /data
  chown -R "$owner" /data
' restore "$file"; then
  die "could not restore the data; it is in backups/$file. $old was not started"
fi
docker compose up -d --wait || die "rollback to $old also failed; data backup is backups/$file"
die "rolled back to $old with the data from before the update; $new was not applied"
