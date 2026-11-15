#!/usr/bin/env bash
# Generate shell completions for the release archives and packages.
# GoReleaser runs this before building (see .goreleaser.yaml).
set -euo pipefail
cd "$(dirname "$0")/.."
rm -rf completions
mkdir completions
go run ./cmd/clavis completion bash >completions/clavis.bash
go run ./cmd/clavis completion zsh >completions/_clavis
go run ./cmd/clavis completion fish >completions/clavis.fish
