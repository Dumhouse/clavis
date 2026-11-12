export type DocSnippet = {
  /** Optional tab label when a step offers several client variants. */
  label?: string
  code: string
}

export type DocStep = {
  title: string
  body: string
  snippets?: DocSnippet[]
}

export type DocGuide = {
  id: string
  title: string
  summary: string
  tags: string[]
  steps: DocStep[]
  note?: { title: string; body: string }
}

/** In-app guides. Keep commands in sync with docs/commands.md. */
export const guides: DocGuide[] = [
  {
    id: 'start',
    title: 'First 10 minutes',
    summary: 'Sign in, link a repository, add a secret, and run your app with it.',
    tags: ['login', 'init', 'install', 'secrets', 'run', 'getting started'],
    steps: [
      {
        title: 'Sign in',
        body: 'The CLI uses a browser device-code flow and stores the token in your OS keychain. Use --no-browser on a remote machine.',
        snippets: [
          { code: 'clavis login --server http://localhost:8080' },
          { code: 'clavis whoami' },
        ],
      },
      {
        title: 'Link this repository to a project',
        body: 'This writes .clavis.yaml (project, environment defaults) — commit it. It never contains secret values.',
        snippets: [{ code: 'clavis init --project-name "Web App"' }],
      },
      {
        title: 'Add and read secrets',
        body: 'Create keys in the web grid, or from the terminal. Listing shows names only; revealing values is audited.',
        snippets: [
          { code: 'clavis secrets set DATABASE_URL' },
          { code: 'clavis secrets ls\nclavis secrets get DATABASE_URL --reveal' },
          { code: 'clavis secrets generate STRIPE_SECRET_KEY --type apikey' },
        ],
      },
      {
        title: 'Run your app with secrets',
        body: 'Secrets are injected as environment variables and masked in output. export writes them to a file or stdout when a tool needs that instead.',
        snippets: [
          { code: 'clavis run -- npm run dev' },
          { code: 'clavis run --watch -- npm run dev' },
          { code: 'clavis export --format dotenv -o .env.local' },
        ],
      },
      {
        title: 'CI and non-interactive machines',
        body: 'A CI runner cannot complete the browser login. Create a machine identity scoped to one project and environment, then set its token as CLAVIS_TOKEN. If the repo commits .clavis.yaml, that is the only variable a job needs; add CLAVIS_SERVER when the instance is not on localhost.',
        snippets: [
          {
            code: 'clavis identities create ci-deploy --role Developer --project web-app --identity-env staging',
          },
          { code: 'export CLAVIS_TOKEN=clv_mt_…\nclavis run -- npm test' },
        ],
      },
    ],
    note: {
      title: 'Never commit values',
      body: 'Commit .clavis.yaml and clavis.schema.yaml; keep .env files, tokens, and anything containing values out of git. clavis scan --install-hook is a good pre-commit backstop.',
    },
  },
  {
    id: 'schema',
    title: 'Typed schema and CI',
    summary: 'Declare types, required environments, patterns, and enums — then gate CI on them.',
    tags: ['schema', 'check', 'codegen', 'types', 'ci', 'validation', 'zod', 'pydantic'],
    steps: [
      {
        title: 'Draft a schema from existing keys',
        body: 'Inference suggests types from current values and never writes values into the file. Review the guesses before committing.',
        snippets: [
          { code: 'clavis schema infer -o clavis.schema.yaml' },
          { code: 'clavis schema push --dry-run && clavis schema push' },
        ],
      },
      {
        title: 'Edit the schema',
        body: 'Use the Schema page on a project for type, kind, required-in-environment, pattern, description, example, and enum values. The grid validates edits inline.',
      },
      {
        title: 'Gate CI',
        body: 'check reports missing required keys, type/pattern/enum failures, and keys outside the schema. It exits with code 6 on failure, so it works as a CI step.',
        snippets: [
          { code: 'clavis check --env production' },
          { code: 'clavis check --all --json' },
        ],
      },
      {
        title: 'Generate typed loaders',
        body: 'Emit a zod schema, a Go struct plus loader, or a pydantic model from the same contract and commit the generated file.',
        snippets: [
          { code: 'clavis codegen --lang ts -o src/config.ts' },
          { code: 'clavis codegen --lang go --package config -o internal/config/config.go' },
          { code: 'clavis codegen --lang python -o app/config.py' },
        ],
      },
    ],
  },
  {
    id: 'agents',
    title: 'AI agents and MCP',
    summary: 'Give Claude Code, Cursor, Codex, or Windsurf a narrow, auditable way to use secrets.',
    tags: ['agents', 'mcp', 'claude', 'cursor', 'codex', 'windsurf', 'allowlist', 'ai'],
    steps: [
      {
        title: 'Create an agent',
        body: 'Agent tokens use the clv_at_ prefix, get only read-names + use, expire after one day by default (seven max), and are denied on staging and production.',
        snippets: [
          {
            code: 'clavis identities create coding-agent --type agent --project web-app --identity-env "*"',
          },
        ],
      },
      {
        title: 'Approve commands',
        body: 'The allowlist starts deny-all. Patterns support * and ?; a bare * is rejected. An agent cannot change its own allowlist, and every denied or allowed command is audited.',
        snippets: [
          { code: 'clavis agents allow coding-agent "go test ./..." "npm test" "pnpm dev"' },
          { code: 'clavis agents revoke coding-agent --clear' },
        ],
      },
      {
        title: 'Point your MCP client at it',
        body: 'Run clavis login once, then start clavis mcp with --agent. It uses your login to start a one-hour agent session, keeps the token in memory and renews it. The MCP config holds no secret, so it is safe to commit, and logging out ends the session. Headless agents can still use a static agent token in CLAVIS_TOKEN.',
        snippets: [
          { label: 'Claude Code', code: 'claude mcp add clavis -- clavis mcp --agent claude-code' },
          {
            label: 'Cursor',
            code: '{\n  "mcpServers": {\n    "clavis": {\n      "command": "clavis",\n      "args": ["mcp", "--agent", "cursor"]\n    }\n  }\n}',
          },
          {
            label: 'Codex',
            code: '[mcp_servers.clavis]\ncommand = "clavis"\nargs = ["mcp", "--agent", "codex"]',
          },
          {
            label: 'Windsurf',
            code: '{\n  "mcpServers": {\n    "clavis": {\n      "command": "clavis",\n      "args": ["mcp", "--agent", "windsurf"]\n    }\n  }\n}',
          },
        ],
      },
      {
        title: 'Tools the agent gets',
        body: 'list_secrets returns key names, kinds, schema types, required environments, and descriptions — never values. check_env reports schema coverage. run_with_secrets checks the allowlist, decrypts, runs, and masks output. request_secret files an audited human review request.',
      },
    ],
    note: {
      title: 'Agent limitations',
      body: 'An allowed command receives the secrets: if it runs agent-written code, that code can print or send values. Masking is best-effort and can be evaded by encoding. Allowlist patterns are prefix-style, not argument-level policy. Clavis controls what reaches the agent through MCP; an agent with an unrestricted shell running as you can do anything you can, so run agents in their sandbox. Clavis does not restrict filesystem, network, or process access. Development and preview are still real environments; keep high-value credentials in dynamic, short-lived issuance and out of agent scope.',
    },
  },
  {
    id: 'sync',
    title: 'Sync and import',
    summary:
      'Push secrets to platforms, or migrate in from another tool without a big-bang cutover.',
    tags: [
      'sync',
      'import',
      'vercel',
      'netlify',
      'aws',
      'gcp',
      'vault',
      'cloudflare',
      'doppler',
      'infisical',
    ],
    steps: [
      {
        title: 'Create a sync',
        body: 'Syncs write resolved values to a provider. Pick the provider with --provider; generic HTTP targets receive a signed payload.',
        snippets: [
          {
            code: 'clavis syncs create dev --provider vercel --vercel-project prj_123 --vercel-token "$VERCEL_TOKEN"',
          },
          {
            code: 'clavis syncs create dev --provider aws --aws-region eu-central-1 --aws-service ssm',
          },
          {
            code: 'clavis syncs create dev --provider vault --vault-mount secret --vault-path apps/web',
          },
          { code: 'clavis syncs create dev --endpoint https://deploy.example.com/hooks/clavis' },
        ],
      },
      {
        title: 'Preview, run, and detect drift',
        body: 'The first push requires confirmation. Drift compares local values with providers that can be read back. Optionally delete remote keys that no longer exist in Clavis.',
        snippets: [
          { code: 'clavis syncs run ID --dry-run' },
          { code: 'clavis syncs run ID --yes' },
          { code: 'clavis syncs drift ID\nclavis syncs behavior ID --delete-missing' },
        ],
      },
      {
        title: 'Import from another tool',
        body: 'Preview is always a dry run first. Importers read files or dotenv directories and pull from Infisical, Doppler, Vault/OpenBao KV v1/v2, 1Password, AWS Secrets Manager, and GCP Secret Manager.',
        snippets: [
          { code: 'clavis import ./config/ --dry-run' },
          { code: 'clavis import --provider infisical --dry-run' },
          { code: 'clavis import --provider vault --remote-path apps/web --dry-run' },
        ],
      },
    ],
  },
  {
    id: 'workflow',
    title: 'Environments and promotion',
    summary:
      'Compare environments, promote selected keys, and protect production with freezes and approvals.',
    tags: ['environments', 'promote', 'diff', 'overlay', 'branch', 'freeze', 'approval'],
    steps: [
      {
        title: 'Diff before you promote',
        body: 'The diff marks added, changed, missing, and unchanged keys. The Promote page shows the same plan with per-key selection and schema errors.',
        snippets: [
          { code: 'clavis env diff staging production' },
          {
            code: 'clavis promote --from staging --to production --keys DATABASE_URL,FEATURE_FLAGS',
          },
        ],
      },
      {
        title: 'Protected targets need a request',
        body: 'For protected environments, promote --request creates a change request instead of writing immediately. An Admin or Owner approves it, and the promotion runs with a snapshot.',
        snippets: [{ code: 'clavis promote --from staging --to production --request' }],
      },
      {
        title: 'Overlays and branch environments',
        body: 'Overlays resolve per-region or per-tenant values without duplicating every key. Branch environments can be created from a git branch and inherit from their parent.',
        snippets: [
          { code: 'clavis --overlay eu run -- npm run build' },
          { code: 'clavis env branch feat/new-checkout' },
        ],
      },
      {
        title: 'Freeze a window',
        body: 'Freezes block writes, promotions, and syncs until they end. Break-glass access is the audited escape hatch.',
        snippets: [
          { code: 'clavis env freeze --reason "Release freeze" --hours 4' },
          { code: 'clavis env freezes\nclavis env unfreeze FREEZE_ID' },
        ],
      },
    ],
  },
  {
    id: 'dynamic',
    title: 'Dynamic secrets and rotation',
    summary:
      'Issue short-lived database and AWS credentials, and rotate long-lived secrets on a schedule.',
    tags: ['lease', 'dynamic', 'postgres', 'aws', 'rotation', 'sts', 'credentials'],
    steps: [
      {
        title: 'Run with a lease',
        body: 'Dynamic configs create real database users or AWS credentials with a TTL. lease run revokes the credential automatically when the command exits.',
        snippets: [
          {
            code: 'clavis lease config-create --host db.internal --database app --username admin --password "$PGPASS"',
          },
          { code: 'clavis lease run CONFIG_ID -- psql "$DATABASE_URL"' },
          { code: 'clavis lease renew LEASE_ID\nclavis lease revoke LEASE_ID' },
        ],
      },
      {
        title: 'Schedule rotation',
        body: 'Rotation runs two phases: create the new credential, sync it, verify, then revoke the old one. Failures roll back. Providers cover PostgreSQL passwords, AWS access keys, and a generic webhook.',
        snippets: [
          { code: 'clavis rotation create DB_PASSWORD --provider postgres --interval 24h' },
          { code: 'clavis rotation status --stale-days 90' },
          { code: 'clavis rotation ls' },
        ],
      },
    ],
  },
  {
    id: 'security',
    title: 'Audit, scanning, and sharing',
    summary:
      'Verify the audit chain, catch leaked tokens before they land, and share a single secret safely.',
    tags: ['audit', 'scan', 'share', 'security', 'cache', 'offline'],
    steps: [
      {
        title: 'Scan for leaked tokens',
        body: 'Token-format scanning works for everyone. Matching real values requires read-values and happens in memory. The hook runs on staged changes.',
        snippets: [{ code: 'clavis scan --staged' }, { code: 'clavis scan --install-hook' }],
      },
      {
        title: 'Verify the audit log',
        body: 'Audit entries are hash-chained. ls lists events; verification recomputes the chain so tampering or gaps are visible.',
        snippets: [{ code: 'clavis audit ls --limit 200\nclavis admin audit verify' }],
      },
      {
        title: 'Share one secret',
        body: 'Share links are encrypted in the browser; the key lives in the URL fragment and never reaches the server. Add a view limit, expiry, and optional password.',
        snippets: [
          { code: 'clavis share STRIPE_SECRET_KEY --ttl 24h --views 1 --prompt-password' },
        ],
      },
      {
        title: 'Work offline',
        body: 'The encrypted cache is opt-in per repository. Add cache: true to .clavis.yaml to keep the last resolved set in the OS keychain for 24 hours; clavis run falls back to it when the server is unreachable. CLAVIS_CACHE_DIR changes where the cache is stored.',
        snippets: [
          { code: '# .clavis.yaml\ncache: true' },
          { code: 'CLAVIS_CACHE_DIR=~/.cache/clavis clavis run -- npm test' },
        ],
      },
    ],
  },
]
