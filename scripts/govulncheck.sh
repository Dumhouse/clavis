#!/usr/bin/env bash
# Runs govulncheck and fails on reachable vulnerabilities except those listed
# in .govulncheck-ignore (one "ID  reason" per line). Requires jq.
set -euo pipefail

ignore_file="$(dirname "$0")/../.govulncheck-ignore"
ignored="$(grep -vE '^\s*(#|$)' "$ignore_file" | awk '{print $1}' | jq -R . | jq -s .)"

out="$(mktemp)"
trap 'rm -f "$out"' EXIT
govulncheck -format json ./... >"$out"

# A "finding" whose trace has a function frame means the code calls the vuln.
reachable="$(jq -s --argjson ignored "$ignored" '
  [ .[] | select(.finding != null)
        | select(.finding.trace[0].function != null)
        | .finding.osv ] | unique - $ignored' "$out")"

if [ "$reachable" != "[]" ]; then
  echo "Reachable vulnerabilities not in .govulncheck-ignore: $reachable"
  govulncheck ./... || true
  exit 1
fi
echo "govulncheck: no reachable vulnerabilities (ignored: $ignored)"
