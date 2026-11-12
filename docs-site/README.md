# Docs site

VitePress site built from `../docs`. The build copies docs into `.docs` so
VitePress can resolve its Vue runtime; `.docs` is generated and ignored.

```sh
cd docs-site
pnpm install
pnpm dev
pnpm build
```

The generated site includes the quickstart, concepts, CLI reference, security
whitepaper, operator/CSI docs, supply-chain notes, migration guides, and API
stability policy. Search is local and offline-friendly.
