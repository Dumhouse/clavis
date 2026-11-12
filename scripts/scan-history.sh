#!/bin/sh
# Scan every commit for high-risk credential patterns without printing values.
# Fixture/mockup/test paths are reported separately because they intentionally
# contain fake tokens and placeholder keys.
set -eu
patterns='clv_(mt|at|pat)_[A-Za-z0-9]{8,}|AKIA[0-9A-Z]{16}|-----BEGIN (RSA |OPENSSH |EC |DSA )?PRIVATE KEY-----|sk_live_[A-Za-z0-9]{12,}|ghp_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}'
allow=':(design/mockups/|.*_test\.go|docs/|integrations/|web/src/pages/|web/src/lib/|scripts/scan-history\.sh)'
fixtures=0
unexpected=0
for commit in $(git rev-list --all); do
  matches=$(git grep -lE "$patterns" "$commit" -- . 2>/dev/null || true)
  [ -z "$matches" ] && continue
  real=$(printf '%s\n' "$matches" | grep -vE "$allow" || true)
  if [ -n "$real" ]; then
    echo "Unexpected patterns in $commit:"
    printf '%s\n' "$real" | sed 's/^/  /'
    unexpected=1
  else
    fixtures=1
  fi
done
# Files that should never have been committed.
bad_files=$(git log --all --name-only --pretty=format: -- '*.env' '*.pem' '*.key' '*.db' '*.sqlite' '*.sqlite3' | sed '/^$/d' | sort -u)
if [ -n "$bad_files" ]; then
  echo "Sensitive file types in history:"
  printf '%s\n' "$bad_files" | sed 's/^/  /'
  unexpected=1
fi
if [ "$unexpected" -eq 1 ]; then
  echo "Potential credentials found. Inspect only the listed files/commits; do not paste values into issues." >&2
  exit 1
fi
if [ "$fixtures" -eq 1 ]; then
  echo "No unexpected credential patterns. Fixture/mockup/test placeholders were ignored."
else
  echo "No credential-shaped strings found in git history."
fi
