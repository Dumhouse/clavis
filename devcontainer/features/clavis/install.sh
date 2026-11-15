#!/usr/bin/env bash
# Installs the Clavis CLI into a dev container. The install script checks the
# download's checksum (and its cosign signature when cosign is present).
set -euo pipefail

VERSION="${VERSION:-latest}"

if ! command -v curl >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update -qq && apt-get install -yqq curl ca-certificates
  elif command -v apk >/dev/null 2>&1; then
    apk add --no-cache curl ca-certificates bash tar
  else
    echo "curl is required to install Clavis" >&2
    exit 1
  fi
fi

curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/70840ba6cd99393bd25249e153d9909fa054bcf0/scripts/install.sh \
  | CLAVIS_INSTALL_DIR=/usr/local/bin CLAVIS_VERSION="$VERSION" bash

clavis version
