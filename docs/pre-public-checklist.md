# Pre-public checklist

Complete this before changing the GitHub repository from private to public.

## Repository and history

- [ ] Run `./scripts/scan-history.sh` and review every reported path. The
      current hits are UI/test placeholders; confirm none are live credentials.
- [ ] Run `git log --all --full-history -- '*.env' '*.pem' '*.key' '*.db'` and
      confirm no key material was ever committed.
- [ ] Confirm `.gitignore` covers data directories, databases, keys, `.env`,
      node modules, build output, and test artifacts.
- [ ] Check release assets and CI logs for secrets.
- [ ] Confirm no private customer names, internal URLs, or screenshots in docs.

## Project metadata

- [ ] Repository description, topics, and website set.
- [ ] `LICENSE`, `NOTICE`, `SECURITY.md`, `CODE_OF_CONDUCT.md`,
      `CONTRIBUTING.md`, `GOVERNANCE.md`, `ROADMAP.md`, `CHANGELOG.md`.
- [ ] Issue templates and Discussions categories.
- [ ] Branch protection: required review, status checks, signed commits if
      feasible, no force-push to main.
- [ ] Dependabot/Renovate, CodeQL, secret scanning, and private vulnerability
      reporting enabled.
- [ ] GitHub Pages docs workflow succeeds.
- [ ] Release workflow produces signed artifacts, checksums, SBOMs, and
      provenance.

## Release candidate

- [ ] `go test ./...`, Postgres suites, CSI tests, web checks, and E2E pass.
- [ ] `make build`, `make operator`, `make csi`, and `make docs-site` pass.
- [ ] Backup/restore and Shamir unseal smoke-tested.
- [ ] Load test run and results recorded.
- [ ] Demo instance resets and contains no real credentials.
- [ ] Audit findings closed or explicitly accepted with a public note.

## Launch

- [ ] Discussions/Matrix/Discord links published.
- [ ] Launch drafts reviewed with current screenshots and claims.
- [ ] Listing submissions prepared.
- [ ] Maintainer coverage for launch-day comments and security reports.
