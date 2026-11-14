#!/bin/sh
# Prints the version to stamp into a build.
#   On a clean release tag:  0.3.2
#   Anywhere else:           0.3.2+dev.752e7e5        (latest release + commit)
#   With uncommitted edits:  0.3.2+dev.752e7e5.dirty
# The build time is stamped separately (internal/version.Date).
set -e
dirty=$(git status --porcelain --untracked-files=no 2>/dev/null | head -n 1)
tag=$(git describe --tags --exact-match 2>/dev/null || true)
if [ -n "$tag" ] && [ -z "$dirty" ]; then
	echo "${tag#v}"
	exit 0
fi
latest=$(git tag --list 'v*' --sort=-v:refname 2>/dev/null | head -n 1)
latest=${latest#v}
commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
echo "${latest:-0.0.0}+dev.${commit}${dirty:+.dirty}"
