# Security Policy

Clavis is a secrets manager. We take reports seriously and will never ask you
to disclose a vulnerability in a public issue.

## Reporting a vulnerability

Use GitHub Security Advisories in this repository (Security → Report a
vulnerability). If that is unavailable, email `dumhousestudio@gmail.com`.

Please include:

- Affected version or commit.
- A description of the issue and its impact.
- Reproduction steps or a proof of concept.
- Any suggested fix.

Do **not** include real secret values, production tokens, or customer data. Use
synthetic values in proofs of concept.

## What to expect

- Acknowledgement within 3 business days.
- A triage decision and severity assessment within 10 business days.
- Coordinated disclosure within 90 days of the report, or sooner once a fix is
  available. We will credit reporters unless they prefer to remain anonymous.
- A CVE and a signed release for confirmed vulnerabilities.

## Scope

In scope: the server, CLI, web UI, SDKs, operator, integrations, container
images, and release pipeline in this repository.

Out of scope: vulnerabilities in third-party services, social engineering,
denial of service against a single self-hosted instance, and findings that
require a fully compromised host (see the threat model in
[docs/security.md](docs/security.md)).

## Supported versions

Until 1.0, only the latest release receives security fixes.
