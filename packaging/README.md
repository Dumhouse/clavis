# Packaging

Files for distributing the CLI through channels the release pipeline does not
publish to on its own.

- `aur/PKGBUILD`: Arch Linux (`clavis-bin`, installs the release binary).
  Run `scripts/update-packages.sh <version>` after each release to set the
  version and checksums, then push the PKGBUILD to the AUR yourself
  (`git clone ssh://aur@aur.archlinux.org/clavis-bin.git`). The checked-in
  PKGBUILD uses `SKIP` for checksums until you run the script.
- `../devcontainer/features/clavis`: a Dev Container Feature that installs the
  CLI. Use it from a repository with `"features": {"ghcr.io/dumhouse/clavis/devcontainer/clavis:1": {}}`
  once it is published, or copy the folder into `.devcontainer/` and reference
  it as `"./clavis"`.
- The server image `dumhouse/clavis` also works as a CLI image, because its
  entrypoint is `clavis`: `docker run --rm -e CLAVIS_TOKEN dumhouse/clavis secrets ls`.

Nix is not packaged yet: a `buildGoModule` derivation needs a vendor hash
computed on a machine with Nix.
