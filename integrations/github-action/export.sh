#!/usr/bin/env bash
# Loads one environment into the job: later steps see each key as an
# environment variable, and secret values are masked in the log.
set -euo pipefail

bin="${CLAVIS_BIN:-clavis}"
[ -n "${INPUT_ORG:-}" ] && export CLAVIS_ORG="$INPUT_ORG"
[ -n "${INPUT_PROJECT:-}" ] && export CLAVIS_PROJECT="$INPUT_PROJECT"
[ -n "${INPUT_ENVIRONMENT:-}" ] && export CLAVIS_ENV="$INPUT_ENVIRONMENT"
if [ -n "${INPUT_CA_CERT:-}" ]; then
  if [ -f "$INPUT_CA_CERT" ]; then
    export CLAVIS_CA_CERT="$INPUT_CA_CERT"
  else # PEM text; the auth step wrote it here
    export CLAVIS_CA_CERT="${RUNNER_TEMP:-/tmp}/clavis-ca.pem"
  fi
fi
[ -z "${CLAVIS_SERVER_FINGERPRINT:-}" ] && unset CLAVIS_SERVER_FINGERPRINT

if [ -z "${CLAVIS_PROJECT:-}" ] && [ ! -f .clavis.yaml ]; then
  echo "::error title=Clavis: which project?::Set the project (and org) inputs, or run actions/checkout before this step so the repository's .clavis.yaml is there."
  exit 1
fi

# On failure the CLI prints an error annotation with the cause and the fix
# (rejected token, wrong environment, untrusted certificate, unreachable
# server) and exits non-zero, which fails this step.
exec "$bin" export --format github-env
