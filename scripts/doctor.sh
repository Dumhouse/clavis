#!/usr/bin/env bash
# Local development preflight: toolchain, ports, and instance state.
set -u
status=0

ok() { printf '  \033[32mok\033[0m    %s\n' "$1"; }
warn() { printf '  \033[33mwarn\033[0m  %s\n' "$1"; }
fail() { printf '  \033[31mfail\033[0m  %s\n' "$1"; status=1; }

need() {
  if command -v "$1" >/dev/null 2>&1; then
    ok "$1 $($2 2>/dev/null | head -1)"
  else
    fail "$1 is not installed"
  fi
}

port_busy() { lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }

echo "Toolchain"
need go 'go version'
need node 'node --version'
need pnpm 'pnpm --version'
need git 'git --version'

echo
echo "Ports"
if port_busy 8080; then
  warn "port 8080 is already in use (the API may already be running)"
else
  ok "port 8080 is free"
fi
if port_busy 5173; then
  warn "port 5173 is in use (Vite may already be running)"
else
  ok "port 5173 is free"
fi

echo
echo "Instance"
if [ -f data/clavis.db ]; then
  ok "data/clavis.db exists"
else
  warn "no database yet; the setup wizard will run on first start"
fi
if [ -f data/root.key ]; then
  ok "root key present (keep it out of database backups)"
else
  warn "no root key yet; it is created on first start"
fi
if [ -f data/setup-token ]; then
  warn "setup is still open: use the token in data/setup-token at /setup"
fi

echo
if [ "$status" -eq 0 ]; then
  echo "Ready. Run 'make dev' and open http://localhost:8080"
else
  echo "Fix the failed checks above, then re-run 'make doctor'."
fi
exit "$status"
