#!/usr/bin/env bash
# Makes CLAVIS_* available to later steps, with a static token or by
# exchanging the runner's OIDC token (GitHub only) for a short-lived one.
set -euo pipefail

server="${INPUT_SERVER%/}"
token="${INPUT_TOKEN:-}"
expires=""

if [ -z "$server" ]; then
  echo "::error title=Clavis: no server::The server input is required (for example https://clavis.example.com)."
  exit 1
fi

if [ -z "$token" ]; then
  if [ -z "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ] || [ -z "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ]; then
    echo "::error title=Clavis: empty token::The token input is empty. If you pass \${{ secrets.CLAVIS_TOKEN_… }}, that secret is not set in this repository (Settings → Actions → Secrets; check the exact name), or the run comes from a fork, which gets no secrets. (On GitHub you can instead sign in with OIDC: 'permissions: id-token: write' plus the identity input.)"
    exit 1
  fi
  if [ -z "${INPUT_ORG:-}" ] || [ -z "${INPUT_IDENTITY:-}" ]; then
    echo "::error::Signing in with OIDC needs the org and identity inputs."
    exit 1
  fi
  for tool in curl jq; do
    command -v "$tool" >/dev/null 2>&1 || { echo "::error::OIDC sign-in needs $tool on the runner."; exit 1; }
  done
  oidc_response="$(curl -sS -H "Authorization: bearer ${ACTIONS_ID_TOKEN_REQUEST_TOKEN}" \
    "${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=${INPUT_AUDIENCE:-clavis}")"
  oidc_token="$(printf '%s' "$oidc_response" | jq -r '.value // empty')"
  if [ -z "$oidc_token" ]; then
    echo "::error::The runner did not return an OIDC token."
    exit 1
  fi
  payload="$(jq -nc --arg org "$INPUT_ORG" --arg identity "$INPUT_IDENTITY" --arg token "$oidc_token" \
    '{org: $org, identity: $identity, token: $token}')"
  exchange="$(curl -sS -X POST "${server}/api/v1/auth/oidc" -H 'Content-Type: application/json' -d "$payload")"
  token="$(printf '%s' "$exchange" | jq -r '.token // empty')"
  expires="$(printf '%s' "$exchange" | jq -r '.expires_at // empty')"
  if [ -z "$token" ]; then
    echo "::error::Clavis rejected the OIDC exchange: $(printf '%s' "$exchange" | jq -r '.detail // .title // empty')"
    exit 1
  fi
fi

echo "::add-mask::$token"
{
  echo "CLAVIS_SERVER=$server"
  echo "CLAVIS_TOKEN=$token"
  [ -n "${INPUT_ORG:-}" ] && echo "CLAVIS_ORG=$INPUT_ORG"
  [ -n "${INPUT_PROJECT:-}" ] && echo "CLAVIS_PROJECT=$INPUT_PROJECT"
  [ -n "${INPUT_ENVIRONMENT:-}" ] && echo "CLAVIS_ENV=$INPUT_ENVIRONMENT"
  [ -n "${INPUT_SERVER_FINGERPRINT:-}" ] && echo "CLAVIS_SERVER_FINGERPRINT=$INPUT_SERVER_FINGERPRINT"
  if [ -n "${INPUT_CA_CERT:-}" ]; then
    if [ -f "$INPUT_CA_CERT" ]; then
      echo "CLAVIS_CA_CERT=$INPUT_CA_CERT"
    else # PEM text, e.g. from a secret
      ca="${RUNNER_TEMP:-/tmp}/clavis-ca.pem"
      printf '%s\n' "$INPUT_CA_CERT" > "$ca"
      echo "CLAVIS_CA_CERT=$ca"
    fi
  fi
  true
} >> "${GITHUB_ENV:?GITHUB_ENV is not set}"
{
  echo "token=$token"
  echo "expires-at=$expires"
} >> "${GITHUB_OUTPUT:-/dev/null}"
