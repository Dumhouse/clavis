#!/bin/sh
# Reset the disposable demo instance. Run from the repository root.
set -eu
DIR=${CLAVIS_DEMO_DIR:-./deploy/demo/data}
docker compose -f deploy/demo/docker-compose.yml down -v >/dev/null 2>&1 || true
rm -rf "$DIR"
mkdir -p "$DIR"
docker compose -f deploy/demo/docker-compose.yml up -d
for i in $(seq 1 100); do
  curl -sf http://127.0.0.1:8080/healthz >/dev/null 2>&1 && break
  sleep 0.2
done
echo "Demo reset. Setup token:"
cat "$DIR/setup-token" 2>/dev/null || docker compose -f deploy/demo/docker-compose.yml logs clavis | grep -i setup || true
