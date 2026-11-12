# API stability and deprecation

## Versioning

- Public HTTP routes are under `/api/v1`.
- The OpenAPI document is generated from code and publishes at
  `/api/v1/openapi.json` (plus YAML and OpenAPI 3.0 variants).
- Additive fields and new endpoints are non-breaking and may ship in minor
  releases.
- Removing or renaming a field, changing a type, narrowing an enum, or changing
  authorization requires a new API version.

## Deprecation

1. Mark the field or endpoint deprecated in the OpenAPI contract.
2. Return a `Deprecation` header and document the replacement.
3. Keep the old behavior for at least two minor releases and 180 days.
4. Remove it only in a major release.

## Compatibility checks

- `go test ./internal/server` asserts that every route declares a permission.
- `make gen-api` regenerates the TypeScript client from the live OpenAPI
  contract.
- CI fails if the generated contract or web types drift.
- CLI commands use the same versioned HTTP API; `clavis` and the server support
  the latest two minor releases.

## Stable error format

Errors use RFC 9457 `application/problem+json` with stable `code` values such as
`validation_failed`, `forbidden`, `not_found`, `precondition_failed`,
`promotion_approval_required`, and `agent_command_denied`. Clients should branch
on `code`, not on human-readable `detail`.
