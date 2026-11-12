export type CommandDoc = {
  name: string
  usage: string
  description: string
  flags?: string[]
  examples?: string[]
}

export type CommandGroup = {
  title: string
  description: string
  commands: CommandDoc[]
}

export const commandGroups: CommandGroup[] = [
  {
    title: 'Getting started',
    description: 'Sign in and link a repository to a project.',
    commands: [
      {
        name: 'login',
        usage: 'clavis login [--server URL] [--no-browser]',
        description: 'Sign in with the browser device-code flow and store the token securely.',
        examples: ['clavis login --server https://clavis.example.com'],
      },
      {
        name: 'logout',
        usage: 'clavis logout',
        description: 'Revoke the saved session and delete the local credential.',
      },
      {
        name: 'whoami',
        usage: 'clavis whoami',
        description: 'Show the signed-in user and organizations.',
      },
      {
        name: 'init',
        usage: 'clavis init [--project-name NAME]',
        description: 'Link this directory to a project and write .clavis.yaml.',
        examples: ['clavis init', 'clavis init --project-name "My App"'],
      },
      {
        name: 'link / unlink',
        usage: 'clavis link | clavis unlink',
        description: 'Change the linked project or remove .clavis.yaml.',
      },
    ],
  },
  {
    title: 'Secrets',
    description: 'Read, write, version, and override secrets.',
    commands: [
      {
        name: 'secrets ls',
        usage: 'clavis secrets ls [--reveal]',
        description: 'List key names. --reveal decrypts values and is audited.',
      },
      {
        name: 'secrets get',
        usage: 'clavis secrets get KEY [--reveal]',
        description: 'Print one secret value.',
        examples: ['clavis secrets get DATABASE_URL'],
      },
      {
        name: 'secrets set',
        usage: 'clavis secrets set KEY [VALUE]',
        description: 'Create or update a secret. Omit VALUE to prompt, or use - for stdin.',
        flags: ['--kind secret|variable|file', '--comment TEXT', '--message TEXT', '--yes'],
        examples: ['clavis secrets set API_KEY - < key.txt'],
      },
      {
        name: 'secrets generate',
        usage: 'clavis secrets generate KEY',
        description: 'Generate and store a random value.',
        flags: [
          '--type password|apikey|hex|base64|uuid|jwt|ed25519|rsa',
          '--length N',
          '--kind',
          '--message',
          '--yes',
        ],
        examples: ['clavis secrets generate SESSION_KEY --type password --length 48'],
      },
      {
        name: 'secrets rm',
        usage: 'clavis secrets rm KEY [--yes]',
        description: 'Soft-delete a key; history is kept.',
      },
      {
        name: 'secrets history',
        usage: 'clavis secrets history KEY',
        description: 'Show version history and change messages.',
      },
      {
        name: 'secrets rollback',
        usage: 'clavis secrets rollback KEY --version N',
        description: 'Create a new version from an older one.',
        flags: ['--version N', '--yes'],
      },
      {
        name: 'secrets override',
        usage: 'clavis secrets override KEY [VALUE] [--remove]',
        description: 'Set a personal override for a non-production environment.',
        flags: ['--remove', '--comment TEXT'],
      },
    ],
  },
  {
    title: 'Run and export',
    description: 'Inject secrets into processes, files, and CI.',
    commands: [
      {
        name: '--overlay SLUG',
        usage: 'clavis --overlay SLUG run|export|secrets ...',
        description:
          'Apply a tenant/region overlay to run, export, or secret reads. Overlay values appear as grid sub-columns.',
        examples: ['clavis --overlay eu run -- npm start'],
      },
      {
        name: 'run',
        usage: 'clavis run [--mount] [--no-mask] -- COMMAND [ARGS...]',
        description:
          'Inject secrets as environment variables and mask output. --mount writes file secrets to a private temp directory.',
        flags: ['--mount', '--no-mask', '--watch'],
        examples: [
          'clavis run -- npm run dev',
          'clavis run --mount -- ./start.sh',
          'clavis run --watch -- pnpm dev',
        ],
      },
      {
        name: 'shell',
        usage: 'clavis shell',
        description:
          'Start a subshell with secrets exported and a colored prompt. Values are not masked.',
      },
      {
        name: 'export',
        usage: 'clavis export [--format FORMAT] [-o FILE]',
        description: 'Export an environment as dotenv, docker, json, yaml, shell, or k8s.',
        flags: ['--format dotenv|docker|json|yaml|shell|k8s', '-o, --out FILE', '--variables-only'],
        examples: ['clavis export --format docker > .env'],
      },
      {
        name: 'run --watch / cache',
        usage: 'clavis run --watch -- COMMAND',
        description:
          'Restart on secret changes. With cache: true in .clavis.yaml, run falls back to an encrypted 24h offline cache.',
        examples: ['clavis run --watch -- pnpm dev'],
      },
      {
        name: 'share',
        usage: 'clavis share KEY [--ttl 24h] [--views 1] [--prompt-password]',
        description:
          'Create a client-encrypted one-time link. The key stays in the URL fragment; the server stores ciphertext only.',
        flags: ['--ttl', '--views', '--password', '--prompt-password'],
        examples: ['clavis share DATABASE_URL --ttl 2h --views 1'],
      },
      {
        name: 'scan',
        usage: 'clavis scan [PATH] [--staged] [--install-hook]',
        description:
          'Scan for Clavis token formats and resolved values in memory. Exit code 7 on a leak.',
        flags: ['--staged', '--install-hook', '--values'],
        examples: ['clavis scan --staged', 'clavis scan --install-hook'],
      },
      {
        name: 'import',
        usage: 'clavis import [FILE|DIR] [--provider PROVIDER] [--dry-run]',
        description:
          'Import files, dotenv directories, or pull from Infisical, Doppler, Vault/OpenBao, 1Password, AWS, or GCP. Preview new, changed, and unchanged keys first.',
        flags: [
          '--provider file|infisical|doppler|vault|onepassword|aws|gcp',
          '--map FILE',
          '--folder PATH',
          '--kind secret|variable|file',
          '--dry-run',
        ],
        examples: [
          'clavis import ./config/ --dry-run',
          'clavis import --provider vault --remote-path apps/web --dry-run',
        ],
      },
    ],
  },
  {
    title: 'Schema and validation',
    description: 'Keep a typed contract in the repo and gate CI on it.',
    commands: [
      {
        name: 'check',
        usage: 'clavis check [--env E] [--all] [--json]',
        description:
          'Validate required keys, types, patterns, and enum values. Missing required keys or invalid values exit with code 6.',
        flags: ['--env E', '--all', '--json'],
        examples: ['clavis check --env production', 'clavis check --all --json'],
      },
      {
        name: 'schema ls',
        usage: 'clavis schema ls',
        description: 'List schema entries and their required environments.',
      },
      {
        name: 'schema pull',
        usage: 'clavis schema pull [-o FILE]',
        description: 'Download the schema as YAML (default clavis.schema.yaml).',
        examples: ['clavis schema pull -o config/clavis.schema.yaml'],
      },
      {
        name: 'schema push',
        usage: 'clavis schema push [FILE] [--dry-run]',
        description: 'Validate and upload a schema file.',
        flags: ['--dry-run'],
        examples: ['clavis schema push config/clavis.schema.yaml --dry-run'],
      },
      {
        name: 'schema infer',
        usage: 'clavis schema infer [-o FILE] [--push]',
        description:
          'Suggest types from current values without writing secret values into the file.',
        flags: ['-o, --out FILE', '--push'],
      },
      {
        name: 'codegen',
        usage: 'clavis codegen --lang ts|go|python [-o FILE]',
        description:
          'Generate a zod schema, Go struct and loader, or pydantic model from the project schema.',
        flags: ['--lang ts|go|python', '-o, --out FILE', '--package NAME', '--name NAME'],
        examples: [
          'clavis codegen --lang ts -o src/config.ts',
          'clavis codegen --lang go --package config -o internal/config/config.go',
        ],
      },
    ],
  },
  {
    title: 'Environments',
    description: 'Manage environment definitions and compare them.',
    commands: [
      {
        name: 'env ls',
        usage: 'clavis env ls',
        description: 'List environments in the linked project.',
      },
      {
        name: 'env create',
        usage: 'clavis env create NAME [--slug SLUG] [--type TYPE]',
        description: 'Create a development, preview, staging, or production environment.',
        examples: ['clavis env create QA --slug qa --type staging'],
      },
      {
        name: 'env freeze',
        usage: 'clavis env freeze --reason TEXT --hours N [--start RFC3339]',
        description: 'Block writes, promotions, and syncs for the environment.',
      },
      {
        name: 'env freezes|unfreeze',
        usage: 'clavis env freezes | clavis env unfreeze FREEZE_ID',
        description: 'List or remove freeze windows.',
      },
      {
        name: 'env branch',
        usage: 'clavis env branch [BRANCH]',
        description:
          'Create or refresh a branch preview environment with inheritance and inactivity expiry.',
        examples: ['clavis env branch', 'clavis env branch feature/checkout'],
      },
      {
        name: 'env diff',
        usage: 'clavis env diff SOURCE TARGET',
        description: 'Compare key names between two environments.',
        examples: ['clavis env diff dev prod'],
      },
      {
        name: 'promote',
        usage: 'clavis promote --from SOURCE --to TARGET [--keys ...] [--request]',
        description:
          'Diff, schema-check, snapshot, and copy selected keys. Protected targets use approval requests.',
        flags: ['--from', '--to', '--keys', '--all', '--interactive', '--snapshot', '--request'],
        examples: ['clavis promote --from dev --to staging --interactive'],
      },
    ],
  },
  {
    title: 'Shared sets',
    description: 'Org-level secret sets imported by environments.',
    commands: [
      { name: 'sets ls', usage: 'clavis sets ls', description: 'List shared sets.' },
      {
        name: 'sets show',
        usage: 'clavis sets show SET [--reveal]',
        description: 'Show set keys; --reveal decrypts values.',
      },
      {
        name: 'sets create',
        usage: 'clavis sets create NAME --slug SLUG',
        description: 'Create a shared set with its own encryption key.',
      },
      {
        name: 'sets set',
        usage: 'clavis sets set SET KEY [VALUE]',
        description: 'Create or update a value inside a set.',
      },
      { name: 'sets rm', usage: 'clavis sets rm SET KEY', description: 'Remove a key from a set.' },
      {
        name: 'sets import',
        usage: 'clavis sets import SET [--env E]',
        description: 'Import a set into an environment. Local values win.',
      },
      {
        name: 'sets detach',
        usage: 'clavis sets detach SET [--env E]',
        description: 'Remove a set import from an environment.',
      },
    ],
  },
  {
    title: 'Syncs',
    description: 'Push resolved secrets to external systems.',
    commands: [
      {
        name: 'syncs ls',
        usage: 'clavis syncs ls',
        description: 'List syncs for the linked project.',
      },
      {
        name: 'syncs create',
        usage: 'clavis syncs create ENVIRONMENT --endpoint URL',
        description: 'Create a signed generic HTTP sync.',
        flags: [
          '--endpoint URL',
          '--secret VALUE',
          '--provider http|gitlab|github|vercel|aws|gcp|netlify|cloudflare|vault',
          '--gitlab-project PATH',
          '--gitlab-token TOKEN',
          '--github-repo OWNER/NAME',
          '--github-token TOKEN',
          '--github-app-id ID',
          '--github-installation-id ID',
          '--github-private-key FILE',
          '--vercel-project ID',
          '--vercel-token TOKEN',
          '--aws-region REGION',
          '--aws-service ssm|secretsmanager',
          '--gcp-project ID',
          '--netlify-account SLUG',
          '--cloudflare-account ID',
          '--forgejo-repo OWNER/NAME',
          '--delete-missing',
        ],
        examples: [
          'clavis syncs create dev --endpoint https://deploy.example.com/hooks/clavis',
          'clavis syncs create dev --provider gitlab --gitlab-project group/project --gitlab-token "$GITLAB_TOKEN"',
          'clavis syncs create dev --provider github --github-repo acme/repo --github-token "$GITHUB_TOKEN"',
          'clavis syncs create dev --provider forgejo --forgejo-url https://git.example.com --forgejo-repo team/web --forgejo-token "$FORGEJO_TOKEN"',
          'clavis syncs create dev --provider vercel --vercel-project prj_123 --vercel-token "$VERCEL_TOKEN"',
          'clavis syncs create dev --provider aws --aws-region eu-central-1 --aws-service ssm',
          'clavis syncs create dev --provider gcp --gcp-project my-project --gcp-service-account-json @service-account.json',
          'clavis syncs create dev --provider netlify --netlify-account acme --netlify-token "$NETLIFY_TOKEN"',
          'clavis syncs create dev --provider cloudflare --cloudflare-account "$CF_ACCOUNT" --cloudflare-script my-worker --cloudflare-token "$CF_TOKEN"',
        ],
      },
      {
        name: 'syncs run',
        usage: 'clavis syncs run ID [--dry-run] [--yes]',
        description: 'Preview or execute a sync. The first push requires confirmation.',
        flags: ['--dry-run', '--yes'],
      },
      {
        name: 'syncs behavior',
        usage: 'clavis syncs behavior ID [--delete-missing]',
        description: 'Toggle deleting remote keys that are not in Clavis.',
      },
      {
        name: 'syncs drift',
        usage: 'clavis syncs drift ID',
        description: 'Compare local values with a readable remote provider.',
      },
      {
        name: 'syncs logs',
        usage: 'clavis syncs logs ID',
        description: 'Show recent run status, key counts, and errors.',
      },
    ],
  },
  {
    title: 'Organization and access',
    description: 'Inspect people, groups, and effective permissions.',
    commands: [
      {
        name: 'members ls',
        usage: 'clavis members ls',
        description: 'List organization members and their roles.',
      },
      {
        name: 'access explain',
        usage: 'clavis access explain <user|group> --permission PERM',
        description: 'Explain why a subject has or does not have access.',
        flags: ['--permission', '--project', '--environment', '--path', '--user', '--group'],
        examples: [
          'clavis access explain ada@example.com --permission secrets:write --env prod',
          'clavis access explain platform-team --permission secrets:read-values',
        ],
      },
    ],
  },
  {
    title: 'Identities',
    description: 'Machine tokens and OIDC workload federation.',
    commands: [
      {
        name: 'identities ls',
        usage: 'clavis identities ls',
        description: 'List machine and workload identities.',
      },
      {
        name: 'identities create',
        usage: 'clavis identities create NAME [flags]',
        description: 'Create an identity and its first token.',
        flags: [
          '--type machine|agent',
          '--role ROLE',
          '--project SLUG',
          '--identity-env GLOB',
          '--oidc github|gitlab|generic',
          '--issuer URL',
          '--audience AUD',
          '--repo OWNER/NAME',
          '--branch BRANCH',
          '--gh-environment ENV',
          '--k8s-namespace NS',
          '--k8s-service-account NAME',
          '--gcp-service-account EMAIL',
          '--aws-account ID',
          '--aws-arn GLOB',
          '--aws-user-id GLOB',
          '--claim key=value',
        ],
        examples: [
          'clavis identities create deploy-bot --role Developer --project web-app',
          'clavis identities create github-actions --oidc github --repo acme/repo --branch main',
          'clavis identities create gitlab-ci --oidc gitlab --repo group/project --branch main',
          'clavis identities create k8s-deployer --oidc kubernetes --k8s-namespace apps --k8s-service-account deployer',
          'clavis identities create gcp-deployer --oidc gcp --gcp-service-account deployer@project.iam.gserviceaccount.com',
        ],
      },
      {
        name: 'identities revoke',
        usage: 'clavis identities revoke NAME',
        description: 'Delete an identity and revoke its tokens.',
      },
    ],
  },
  {
    title: 'Dynamic secrets',
    description: 'Short-lived database credentials with leases and automatic revocation.',
    commands: [
      {
        name: 'lease config-create',
        usage:
          'clavis lease config-create --host HOST --database DB --username USER --password PASS',
        description: 'Create a PostgreSQL or AWS STS dynamic secret config.',
      },
      {
        name: 'lease create',
        usage: 'clavis lease create CONFIG_ID [--ttl SECONDS]',
        description: 'Issue short-lived credentials. The password is shown once.',
      },
      {
        name: 'lease run',
        usage: 'clavis lease run CONFIG_ID -- COMMAND',
        description: 'Run a command with dynamic credentials and auto-revoke afterwards.',
      },
      {
        name: 'lease renew / revoke',
        usage: 'clavis lease renew LEASE_ID | clavis lease revoke LEASE_ID',
        description: 'Extend or revoke a lease.',
      },
    ],
  },
  {
    title: 'Offboarding',
    description: 'Review secrets revealed by a departing member and track rotation.',
    commands: [
      {
        name: 'members offboarding',
        usage: 'clavis members offboarding USER [--start]',
        description: 'Show or create the offboarding review with progress and export counts.',
      },
      {
        name: 'members mark-safe|mark-rotated',
        usage: 'clavis members mark-safe USER TASK_ID | clavis members mark-rotated USER TASK_ID',
        description: 'Mark a revealed secret reviewed as safe or rotated.',
      },
    ],
  },
  {
    title: 'Health',
    description: 'Security and hygiene findings with a health score.',
    commands: [
      {
        name: 'health',
        usage: 'clavis health [--project SLUG] [--json]',
        description: 'Report stale, unrotated, expiring, no-MFA, and non-expiring token findings.',
      },
    ],
  },
  {
    title: 'Just-in-time access',
    description: 'Temporary role requests with reasons, multiple approvers, and expiry.',
    commands: [
      {
        name: 'access request',
        usage: 'clavis access request --env E --role ROLE --duration 1h --reason TEXT',
        description: 'Request temporary access. The final approval creates a time-bound grant.',
      },
      {
        name: 'access requests',
        usage: 'clavis access requests [--status pending|approved|rejected]',
        description: 'List access requests. Non-managers see only their own.',
      },
      {
        name: 'access review',
        usage: 'clavis access review --env prod [--format csv]',
        description: 'Report who can read values in an environment, with CSV export.',
      },
      {
        name: 'access break-glass',
        usage: 'clavis access break-glass --env E --reason TEXT --duration 1h',
        description: 'Immediately grant Admin access, lift an active freeze, and audit loudly.',
      },
      {
        name: 'access approve|reject',
        usage: 'clavis access approve REQUEST_ID | clavis access reject REQUEST_ID',
        description: 'Approve or reject a request. Self-approval is blocked.',
      },
    ],
  },
  {
    title: 'Rotation',
    description: 'Two-phase scheduled rotation with rollback and run history.',
    commands: [
      {
        name: 'rotation create',
        usage: 'clavis rotation create KEY --provider postgres|aws|webhook --interval 24h',
        description: 'Schedule rotation for a secret key.',
      },
      {
        name: 'rotation status',
        usage: 'clavis rotation status [--stale-days 90]',
        description: 'Show managed, unmanaged, and stale manual-rotation secrets.',
        flags: ['--stale-days N', '--json'],
      },
      {
        name: 'rotation ls',
        usage: 'clavis rotation ls',
        description: 'List policies, next run, and last status.',
      },
      {
        name: 'rotation run',
        usage: 'clavis rotation run POLICY_ID',
        description: 'Rotate immediately and record the phases.',
      },
      {
        name: 'rotation enable|disable|runs|rm',
        usage: 'clavis rotation enable|disable|runs|rm POLICY_ID',
        description: 'Toggle, inspect, or delete a rotation policy.',
      },
    ],
  },
  {
    title: 'AI agents',
    description: 'Short-lived, allowlisted identities and MCP tooling for coding agents.',
    commands: [
      {
        name: 'agents ls',
        usage: 'clavis agents ls',
        description: 'List agent identities and their allowlist state.',
      },
      {
        name: 'agents allow',
        usage: 'clavis agents allow IDENTITY COMMAND...',
        description: 'Approve command patterns. Patterns support * and ?; bare * is rejected.',
        examples: ['clavis agents allow coding-agent "go test ./..."'],
      },
      {
        name: 'agents revoke',
        usage: 'clavis agents revoke IDENTITY [COMMAND...] [--clear]',
        description: 'Remove command patterns, or return to deny-all with --clear.',
      },
      {
        name: 'mcp',
        usage: 'clavis mcp [--agent NAME]',
        description:
          'Run a stdio MCP server with list_secrets, check_env, run_with_secrets, and request_secret. --agent starts an in-memory one-hour session with your login, so the MCP config holds no token.',
        examples: [
          'clavis mcp --agent claude-code',
          'CLAVIS_TOKEN=clv_at_… clavis mcp  # headless only',
        ],
      },
    ],
  },
  {
    title: 'Audit and administration',
    description: 'Verify audit integrity and operate the instance.',
    commands: [
      {
        name: 'audit ls',
        usage: 'clavis audit ls [--limit N] [--after SEQ] [--json]',
        description: 'List recent audit events. Values never appear.',
        examples: ['clavis audit ls --limit 200 --json'],
      },
      {
        name: 'admin audit verify',
        usage: 'clavis admin audit verify',
        description: 'Recompute the audit hash chain and verify signed checkpoints.',
      },
      {
        name: 'admin audit checkpoint',
        usage: 'clavis admin audit checkpoint',
        description: 'Sign the current audit chain head.',
      },
      {
        name: 'admin shamir split',
        usage: 'clavis admin shamir split --shares 5 --threshold 3 --out DIR [--seal]',
        description: 'Split the file root key into Shamir shares and optionally seal the server.',
      },
      {
        name: 'admin unseal',
        usage: 'clavis admin unseal --share FILE ...',
        description: 'Reconstruct the root key from threshold shares.',
      },
      {
        name: 'admin backup',
        usage: 'clavis admin backup --out FILE',
        description: 'Create an encrypted consistent database snapshot with wrapped keys.',
      },
      {
        name: 'admin restore',
        usage: 'clavis admin restore FILE --yes',
        description: 'Restore an encrypted snapshot while the server is stopped.',
      },
      {
        name: 'admin rotate-root-key',
        usage: 'clavis admin rotate-root-key [--init] [--yes] [--config FILE]',
        description:
          'Initialise or rotate the root key offline. Re-wraps org keys and re-encrypts TOTP secrets.',
      },
      {
        name: 'admin migrate-db',
        usage: 'clavis admin migrate-db --from SQLITE --to DSN',
        description: 'Copy a SQLite database into Postgres.',
        flags: ['--from PATH', '--to DSN', '--force', '--yes'],
      },
      {
        name: 'server',
        usage: 'clavis server [--config FILE]',
        description: 'Run the API, job worker, and embedded web UI.',
      },
      {
        name: 'completion',
        usage: 'clavis completion bash|zsh|fish|powershell',
        description: 'Generate shell completion scripts.',
      },
    ],
  },
]

export const globalFlags: [string, string][] = [
  ['--server URL', 'Clavis server URL'],
  ['--org SLUG', 'Organization slug'],
  ['--project SLUG', 'Project slug'],
  ['--env SLUG', 'Environment slug'],
  ['--path PATH', 'Folder path (default /)'],
  ['--json', 'Machine-readable JSON output'],
  ['--debug', 'Enable debug logging'],
]

export const environmentVariables: [string, string][] = [
  ['CLAVIS_SERVER', 'Server URL'],
  ['CLAVIS_TOKEN', 'CLI token for CI'],
  ['CLAVIS_ORG', 'Default organization'],
  ['CLAVIS_PROJECT', 'Default project'],
  ['CLAVIS_ENV', 'Default environment'],
  ['CLAVIS_CREDENTIAL_FILE', 'Credential file location'],
  ['CLAVIS_CONFIG', 'Server configuration file'],
  ['CLAVIS_DATA_DIR', 'Data directory'],
  ['CLAVIS_ROOT_KEY', 'Root key for the env key provider'],
]

export const exitCodes: [string, string][] = [
  ['0', 'Success'],
  ['1', 'General error'],
  ['2', 'Usage error'],
  ['3', 'Authentication required'],
  ['4', 'Permission denied'],
  ['5', 'Not found'],
  ['6', 'Validation failed'],
  ['7', 'Secret leak detected'],
]
