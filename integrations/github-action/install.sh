#!/usr/bin/env bash
# Installs the clavis CLI into $RUNNER_TEMP and adds it to the job PATH.
# Inputs (env): CLAVIS_VERSION (latest or 0.3.2), CLAVIS_INSTALL (auto|true|false),
# CLAVIS_DOWNLOAD_BASE (release download URL, for mirrors and offline runners).
set -euo pipefail

mode="${CLAVIS_INSTALL:-auto}"
if [ "$mode" = "false" ] || { [ "$mode" = "auto" ] && command -v clavis >/dev/null 2>&1; }; then
  if command -v clavis >/dev/null 2>&1; then
    echo "Using $(clavis version | head -1) from $(command -v clavis)"
    echo "bin=$(command -v clavis)" >> "${GITHUB_OUTPUT:-/dev/null}"
  else
    echo "::error::install is false, but clavis is not on PATH."
    exit 1
  fi
  exit 0
fi

base="${CLAVIS_DOWNLOAD_BASE:-https://github.com/dumhouse/clavis/releases}"
base="${base%/}"
fetch() { # url [output]
  if command -v curl >/dev/null 2>&1; then
    if [ -n "${2:-}" ]; then curl -fsSL --retry 3 -o "$2" "$1"; else curl -fsSL --retry 3 "$1"; fi
  elif command -v wget >/dev/null 2>&1; then
    if [ -n "${2:-}" ]; then wget -q -O "$2" "$1"; else wget -q -O - "$1"; fi
  else
    echo "::error::Installing clavis needs curl or wget on the runner."
    exit 1
  fi
}

version="${CLAVIS_VERSION:-latest}"
version="${version#v}"
if [ "$version" = "latest" ]; then
  # Follow the /latest redirect instead of the API: no token, no rate limit.
  if command -v curl >/dev/null 2>&1; then
    location="$(curl -fsSI "$base/latest" | tr -d '\r' | awk 'tolower($1)=="location:"{print $2}' | tail -1)"
  else
    location="$(wget -S --spider --max-redirect=0 "$base/latest" 2>&1 | awk '/^  Location:/{print $2}' | tail -1)"
  fi
  version="${location##*/}"
  version="${version#v}"
  if [ -z "$version" ] || [ "$version" = "latest" ]; then
    echo "::error::Could not find the latest Clavis release at $base/latest. Set the version input."
    exit 1
  fi
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "::error::Unsupported runner OS $(uname -s); install clavis yourself and set install: false."; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "::error::Unsupported CPU $(uname -m)."; exit 1 ;;
esac

archive="clavis_${version}_${os}_${arch}.tar.gz"
dir="${RUNNER_TEMP:-/tmp}/clavis-${version}"
mkdir -p "$dir"
for file in "$archive" checksums.txt; do
  if ! fetch "$base/download/v${version}/${file}" "$dir/$file"; then
    echo "::error::Could not download $base/download/v${version}/${file}. Check the version input (releases: $base) and that the runner can reach it."
    exit 1
  fi
done

expected="$(awk -v f="$archive" '$2==f || $2=="*"f {print $1}' "$dir/checksums.txt")"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$dir/$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$dir/$archive" | awk '{print $1}')"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "::error::Checksum mismatch for $archive (expected ${expected:-none}, got $actual)."
  exit 1
fi

tar -xzf "$dir/$archive" -C "$dir" clavis
chmod +x "$dir/clavis"
echo "$dir" >> "${GITHUB_PATH:?GITHUB_PATH is not set}"
echo "bin=$dir/clavis" >> "${GITHUB_OUTPUT:-/dev/null}"
echo "Installed $("$dir/clavis" version | head -1) (sha256 verified)"
