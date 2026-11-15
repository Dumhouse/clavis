#!/usr/bin/env bash
# Run a real Clavis server and CLI, and save each demo command's output (with
# colors, from a pseudo-terminal) to OUT_DIR/NN.json for render.mjs.
# Usage: scripts/demo/capture.sh path/to/clavis OUT_DIR
set -euo pipefail
BIN="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
OUT="$(mkdir -p "$2" && cd "$2" && pwd)"
PORT=18333
BASE="http://127.0.0.1:${PORT}"
WORK="$(mktemp -d)"
# A short, readable folder for the demo app, since doctor prints its path.
APP="${DEMO_APP_DIR:-/tmp/shop}"
[ -e "$APP" ] && { echo "$APP already exists; set DEMO_APP_DIR" >&2; exit 1; }
trap 'kill "$PID" 2>/dev/null || true; rm -rf "$WORK" "$APP"' EXIT

CLAVIS_DATA_DIR="$WORK/data" CLAVIS_SERVER_ADDR="127.0.0.1:${PORT}" CLAVIS_SERVER_BASE_URL="$BASE" \
  CLAVIS_DEV=true CLAVIS_WEB_DEV_URL=off CLAVIS_UPDATE_CHECK=false CLAVIS_LOG_LEVEL=error \
  "$BIN" server >"$WORK/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 50); do curl -fs "$BASE/api/v1/setup/status" >/dev/null 2>&1 && break; sleep 0.2; done

JAR="$WORK/cookies"
json() { curl -fsS -b "$JAR" -c "$JAR" -H 'content-type: application/json' "$@"; }
csrf() { awk '/clavis_csrf/{print $7}' "$JAR"; }
json -X POST "$BASE/api/v1/setup" -d "{\"token\":\"$(cat "$WORK/data/setup-token")\",\"email\":\"ada@example.com\",\"name\":\"Ada\",\"password\":\"correct-horse-battery\",\"org_slug\":\"acme\",\"org_name\":\"Acme\",\"project_slug\":\"shop\",\"project_name\":\"Shop\"}" >/dev/null
put() { json -X POST -H "X-CSRF-Token: $(csrf)" "$BASE/api/v1/orgs/acme/projects/shop/environments/$1/secrets" -d "{\"key\":\"$2\",\"value\":\"$3\",\"kind\":\"secret\"}" >/dev/null; }
put dev DATABASE_URL "postgres://shop:dev-password@localhost/shop"
put dev STRIPE_KEY "demo_stripe_shared_value"
put dev STRIPE_BACKUP_KEY "demo_stripe_shared_value"
put dev REDIS_URL "redis://localhost:6379/0"
put staging DATABASE_URL "postgres://shop:staging-password@db.staging/shop"
put staging STRIPE_KEY "demo_stripe_staging_value"
put staging REDIS_URL "redis://redis.staging:6379/0"
put prod DATABASE_URL "postgres://shop:prod-password@db.prod/shop"
put prod STRIPE_KEY "demo_stripe_shared_value"
put prod REDIS_URL "redis://redis.prod:6379/0"
TOKEN="$(json -X POST -H "X-CSRF-Token: $(csrf)" "$BASE/api/v1/orgs/acme/tokens" \
  -d '{"permissions":["secrets:read-names","secrets:read-values","envs:read","projects:read","org:read"],"expires_in_days":1}' |
  sed -n 's/.*"token":"\([^"]*\)".*/\1/p' | head -1)"

mkdir -p "$APP" "$WORK/home"
cd "$APP"
printf 'server: %s\norg: acme\nproject: shop\nenv: dev\n' "$BASE" > .clavis.yaml
git init -q && git add .clavis.yaml && git -c user.email=a@example.com -c user.name=a commit -qm init
printf '#!/bin/sh\n# Installed by clavis scan --install-hook\nexec clavis scan --staged\n' > .git/hooks/pre-commit

export HOME="$WORK/home" XDG_CONFIG_HOME="$WORK/home" CLAVIS_TOKEN="$TOKEN" CLAVIS_UPDATE_CHECK=false
export PATH="$(dirname "$BIN"):$PATH"
[ "$(basename "$BIN")" = clavis ] || ln -sf "$BIN" "$WORK/clavis"
export PATH="$WORK:$PATH"

n=0
record() {
  n=$((n + 1))
  python3 - "$OUT/$(printf %02d $n).json" "$@" <<'PY'
import json, os, pty, sys, fcntl, termios, struct
out, cmd = sys.argv[1], sys.argv[2]
chunks = []
def read(fd):
    data = os.read(fd, 65536)
    chunks.append(data)
    return data
pid, fd = pty.fork()
if pid == 0:
    os.execvp("sh", ["sh", "-c", cmd])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 22, 96, 0, 0))
try:
    while read(fd):
        pass
except OSError:
    pass
os.waitpid(pid, 0)
text = b"".join(chunks).decode("utf-8", "replace").replace("\r\n", "\n")
json.dump({"command": cmd, "output": text}, open(out, "w"))
PY
}
record 'clavis run -- sh -c '"'"'echo "Connecting to $DATABASE_URL"'"'"''
record 'clavis run --reveal REDIS_URL -- sh -c '"'"'echo "Cache: $REDIS_URL"'"'"''
record 'clavis secrets duplicates'
record 'clavis env similar'
record 'clavis doctor'
echo "captured $n commands into $OUT"
