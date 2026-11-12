#!/bin/sh
set -eu
if [ $# -lt 2 ]; then
  echo "usage: $0 ARTIFACT BUNDLE [checksums.txt]" >&2
  exit 1
fi
artifact=$1
bundle=$2
cosign verify-blob \
  --bundle "$bundle" \
  --certificate-identity-regexp 'https://github.com/dumhouse/clavis/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "$artifact"
if [ $# -ge 3 ]; then
  grep "  $(basename "$artifact")$" "$3" | sha256sum -c -
fi
echo "verified $artifact"
