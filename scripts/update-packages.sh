#!/usr/bin/env bash
# Point packaging/aur/PKGBUILD at a release and fill in its SHA-256 sums from
# the release's checksums.txt. Usage: scripts/update-packages.sh 0.3.4
set -euo pipefail
version="${1:?usage: update-packages.sh VERSION (for example 0.3.4)}"
version="${version#v}"
cd "$(dirname "$0")/.."
sums="$(curl -fsSL "https://github.com/Dumhouse/clavis/releases/download/v${version}/checksums.txt")"
sum_for() { printf '%s\n' "$sums" | awk -v f="clavis_${version}_linux_$1.tar.gz" '$2 == f {print $1}'; }
amd64="$(sum_for amd64)"
arm64="$(sum_for arm64)"
[ -n "$amd64" ] && [ -n "$arm64" ] || { echo "no linux checksums found for v${version}" >&2; exit 1; }
sed -i.bak \
  -e "s/^pkgver=.*/pkgver=${version}/" \
  -e "s/^pkgrel=.*/pkgrel=1/" \
  -e "s/^sha256sums_x86_64=.*/sha256sums_x86_64=('${amd64}')/" \
  -e "s/^sha256sums_aarch64=.*/sha256sums_aarch64=('${arm64}')/" \
  packaging/aur/PKGBUILD
rm -f packaging/aur/PKGBUILD.bak
echo "Updated packaging/aur/PKGBUILD to v${version}."
