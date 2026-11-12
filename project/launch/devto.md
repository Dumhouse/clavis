# dev.to draft

## Why we built another secrets manager

Most secret tools are either a platform project or a SaaS seat license. We wanted
something a small team could run as one binary, and something safe enough to put
in front of a coding agent.

This post covers:

- the key hierarchy and blind-index design
- typed config + generated loaders
- dynamic credentials vs static values
- why agents get `read-names + use`, not values
- the honest limitation: an allowed command can exfiltrate what it receives
- running Clavis with Docker/Helm and migrating from `.env`/Infisical/Doppler

Repo and docs in the first comment.
