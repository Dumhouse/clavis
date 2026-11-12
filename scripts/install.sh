#!/usr/bin/env bash
# Installs the latest Clavis release binary with checksum and (when available)
# cosign signature verification.
set -euo pipefail

REPO="${CLAVIS_REPO:-dumhouse/clavis}"
VERSION="${CLAVIS_VERSION:-latest}"
INSTALL_DIR="${CLAVIS_INSTALL_DIR:-}"

err() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "missing required command: $1"; }

need curl
need tar
need uname

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  darwin|linux) ;;
  *) err "unsupported operating system: $os" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "unsupported architecture: $arch" ;;
esac

if [ -z "$INSTALL_DIR" ]; then
  if [ -w /usr/local/bin ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="$HOME/.local/bin"
  fi
fi
mkdir -p "$INSTALL_DIR"

if [ "$VERSION" = "latest" ]; then
  api="https://api.github.com/repos/$REPO/releases/latest"
  tag="$(curl -fsSL "$api" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$tag" ] || err "could not determine the latest release; set CLAVIS_VERSION"
else
  tag="$VERSION"
fi
version="${tag#v}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

archive="clavis_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"
printf 'Downloading %s %s (%s/%s)…\n' "$REPO" "$tag" "$os" "$arch"
curl -fsSL "$base/$archive" -o "$tmp/$archive" || err "download failed: $base/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || err "checksum file missing"

expected="$(awk -v file="$archive" '$2 == file {print $1}' "$tmp/checksums.txt")"
[ -n "$expected" ] || err "no checksum for $archive"
actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
[ "$actual" = "$expected" ] || err "checksum mismatch (got $actual, want $expected)"

if command -v cosign >/dev/null 2>&1; then
  if curl -fsSL "$base/${archive}.sigstore.json" -o "$tmp/${archive}.sigstore.json"; then
    cosign verify-blob --bundle "$tmp/${archive}.sigstore.json" \
      --certificate-identity-regexp "https://github.com/$REPO/.*" \
      --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
      "$tmp/$archive" >/dev/null 2>&1 || err "cosign signature verification failed"
    printf 'cosign signature verified.\n'
  fi
fi

tar -xzf "$tmp/$archive" -C "$tmp"
install -m 0755 "$tmp/clavis" "$INSTALL_DIR/clavis"

printf '\nInstalled clavis %s to %s/clavis\n' "$version" "$INSTALL_DIR"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) printf 'Add %s to your PATH to run `clavis`.\n' "$INSTALL_DIR" ;;
esac
"$INSTALL_DIR/clavis" version
