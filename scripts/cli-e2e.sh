#!/usr/bin/env bash
# End-to-end test of the CLI against a real server: set up a server, add a
# secret, then check that clavis run injects and masks it and clavis doctor
# is happy. Usage: scripts/cli-e2e.sh [path/to/clavis]   (default: bin/clavis)
set -euo pipefail

BIN="$(cd "$(dirname "${1:-bin/clavis}")" && pwd)/$(basename "${1:-bin/clavis}")"
PORT="${CLAVIS_E2E_PORT:-18321}"
BASE="http://127.0.0.1:${PORT}"
WORK="$(mktemp -d)"
SERVER_PID=""
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; [ -f "$WORK/server.log" ] && tail -20 "$WORK/server.log" >&2; exit 1; }
ok() { echo "ok: $*"; }

CLAVIS_DATA_DIR="$WORK/data" CLAVIS_SERVER_ADDR="127.0.0.1:${PORT}" CLAVIS_SERVER_BASE_URL="$BASE" \
  CLAVIS_DEV=true CLAVIS_WEB_DEV_URL=off CLAVIS_UPDATE_CHECK=false CLAVIS_LOG_LEVEL=error \
  "$BIN" server >"$WORK/server.log" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 50); do
  curl -fs "$BASE/api/v1/setup/status" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fs "$BASE/api/v1/setup/status" >/dev/null || fail "server did not start"
SETUP_TOKEN="$(cat "$WORK/data/setup-token")"

JAR="$WORK/cookies"
json() { curl -fsS -b "$JAR" -c "$JAR" -H 'content-type: application/json' "$@"; }
csrf() { awk '/clavis_csrf/{print $7}' "$JAR"; }

json -X POST "$BASE/api/v1/setup" -d "{\"token\":\"$SETUP_TOKEN\",\"email\":\"e2e@example.com\",\"name\":\"E2E\",\"password\":\"correct-horse-battery\",\"org_slug\":\"acme\",\"org_name\":\"Acme\",\"project_slug\":\"web\",\"project_name\":\"Web\"}" >/dev/null
ok "first-run setup"

SECRET_VALUE="e2e-secret-value-7f3a9c"
json -X POST -H "X-CSRF-Token: $(csrf)" "$BASE/api/v1/orgs/acme/projects/web/environments/dev/secrets" \
  -d "{\"key\":\"API_KEY\",\"value\":\"$SECRET_VALUE\",\"kind\":\"secret\"}" >/dev/null
OTHER_VALUE="e2e-other-value-b81d44"
json -X POST -H "X-CSRF-Token: $(csrf)" "$BASE/api/v1/orgs/acme/projects/web/environments/dev/secrets" \
  -d "{\"key\":\"OTHER_KEY\",\"value\":\"$OTHER_VALUE\",\"kind\":\"secret\"}" >/dev/null
TOKEN="$(json -X POST -H "X-CSRF-Token: $(csrf)" "$BASE/api/v1/orgs/acme/tokens" -d '{"permissions":["secrets:read-names","secrets:read-values"],"expires_in_days":1}' |
  sed -n 's/.*"token":"\([^"]*\)".*/\1/p' | head -1)"
[ -n "$TOKEN" ] || fail "no token"
ok "secret and token created"

export HOME="$WORK/home" XDG_CONFIG_HOME="$WORK/home"
mkdir -p "$HOME"
export CLAVIS_SERVER="$BASE" CLAVIS_TOKEN="$TOKEN" CLAVIS_ORG=acme CLAVIS_PROJECT=web CLAVIS_ENV=dev CLAVIS_UPDATE_CHECK=false
cd "$WORK"

# The command gets the real value.
got="$("$BIN" run --no-mask -- sh -c 'printf %s "$API_KEY"')"
[ "$got" = "$SECRET_VALUE" ] || fail "run did not inject the secret (got: $got)"
ok "run injects secrets"

# Its output is masked by default.
masked="$("$BIN" run -- sh -c 'echo "key=$API_KEY"')"
case "$masked" in *"$SECRET_VALUE"*) fail "run leaked the value: $masked" ;; esac
case "$masked" in *"••••••"*) ;; *) fail "run did not mask: $masked" ;; esac
ok "run masks output"

# --reveal refuses when output is not a terminal.
if "$BIN" run --reveal API_KEY -- true >/dev/null 2>&1; then fail "--reveal should refuse outside a terminal"; fi
ok "--reveal refuses pipes"

# In a terminal, --reveal shows only the named key, and a hint follows.
shown="$(BIN="$BIN" python3 - <<'PY'
import os, pty, sys
out = []
def read(fd):
    data = os.read(fd, 4096)
    out.append(data)
    return data
status = pty.spawn([os.environ["BIN"], "run", "--reveal", "API_KEY", "sh", "-c", "echo a=$API_KEY b=$OTHER_KEY"], read)
sys.stdout.write(b"".join(out).decode("utf-8", "replace"))
PY
)"
case "$shown" in *"a=$SECRET_VALUE"*) ;; *) fail "--reveal did not show the key: $shown" ;; esac
case "$shown" in *"$OTHER_VALUE"*) fail "--reveal leaked another key: $shown" ;; esac
case "$shown" in *"--reveal KEY"*) ;; *) fail "no masking hint after the session: $shown" ;; esac
ok "--reveal and the masking hint work in a terminal"

"$BIN" doctor >"$WORK/doctor.txt" 2>&1 || { cat "$WORK/doctor.txt"; fail "doctor reported a problem"; }
ok "doctor passes"
echo "CLI end-to-end: all checks passed"
