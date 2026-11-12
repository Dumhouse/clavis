# Contributing to Clavis

Thanks for helping build open-source secrets management. Everything is
Apache-2.0 and there is no enterprise edition.

## Before you start

- Read [docs/vision.md](docs/vision.md), [docs/security.md](docs/security.md),
  and [docs/architecture.md](docs/architecture.md).
- Check the milestone task lists in [tasks/](tasks/). Pick a task or open an
  issue describing what you want to change.
- Security-sensitive work (crypto, authn, authz, audit) needs two reviewers.

## Development setup

Requirements: Go (version in `go.mod`), Node.js (version in `web/.nvmrc`),
pnpm, and optionally `golangci-lint`, `lefthook`, and `sqlc`.

```sh
make tools     # optional: install local developer tools
make web       # build the React app
make test      # Go tests
make lint
make dev       # Go server + Vite with hot reload
```

The Go server proxies SPA requests to Vite in dev mode, and Vite proxies API
requests back to the Go server. Open `http://localhost:8080`.

## Commit sign-off (DCO)

Every commit must be signed off with the Developer Certificate of Origin:

```sh
git commit -s -m "feat: add audit chain verification"
```

The sign-off certifies you wrote the contribution or have the right to submit
it. See <https://developercertificate.org/>.

## Pull requests

1. Keep changes focused and add tests for behavior changes.
2. Run `make fmt lint test`.
3. Describe user-visible behavior and any security implications.
4. Never include real secrets, tokens, or customer data in tests, fixtures,
   screenshots, or logs.
5. Add an entry to the changelog when the project starts shipping releases.

## Security

Please do not open public issues for vulnerabilities. Follow
[SECURITY.md](SECURITY.md).

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
