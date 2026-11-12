# Docs site and landing page

The VitePress site lives in `docs/` and is the project website: a landing page
plus the full documentation. It is deployed to GitHub Pages by
`.github/workflows/docs.yml`.

- `/` — landing page (`docs/index.md`)
- `/quickstart.html` — getting started
- `/screenshots.html` — product tour
- `/commands.html`, `/security.html`, `/agents.html`, …

`pnpm` scripts run `scripts/prepare.mjs`, which copies the shared `brand/`
assets into `public/brand` and writes `.nojekyll`. Everything else is static
content under `docs/`.

## Local development

```sh
make docs-dev           # install, then http://localhost:5174
# or
cd docs
pnpm install
pnpm dev                # http://localhost:5174 (the app uses 5173)
pnpm build              # production build in .vitepress/dist
```

## GitHub Pages

The workflow builds with `DOCS_BASE=/<repo>/` and
`DOCS_HOSTNAME=<pages origin>`, so absolute URLs, the sitemap, and the social
card resolve correctly on project pages.

Repo settings → Pages → Build and deployment → Source: **GitHub Actions**.

If you add a custom domain, set `DOCS_BASE=/` and
`DOCS_HOSTNAME=https://your.domain` in the workflow, and add the domain in
Pages settings.

## Screenshots

Product screenshots live in `docs/assets/screenshots/` and are captured from a
disposable demo instance, not mockups. Regenerate them after UI changes:

```sh
make build
cd web && pnpm shots
```

The script starts a temporary server, seeds realistic schema/secrets/identities,
and writes JPEGs into `docs/assets/screenshots/`. The social card is generated
from `brand/social-card.svg` into `brand/social-card.png`.
