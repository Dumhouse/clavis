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
          '--type password|apikey|hex|base64|uuid',
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
        name: 'run',
        usage: 'clavis run [--mount] [--no-mask] -- COMMAND [ARGS...]',
        description:
          'Inject secrets as environment variables and mask output. --mount writes file secrets to a private temp directory.',
        flags: ['--mount', '--no-mask'],
        examples: ['clavis run -- npm run dev', 'clavis run --mount -- ./start.sh'],
      },
      {
        name: 'export',
        usage: 'clavis export [--format FORMAT] [-o FILE]',
        description: 'Export an environment as dotenv, docker, json, yaml, shell, or k8s.',
        flags: ['--format dotenv|docker|json|yaml|shell|k8s', '-o, --out FILE', '--variables-only'],
        examples: ['clavis export --format docker > .env'],
      },
      {
        name: 'import',
        usage: 'clavis import FILE|DIR',
        description:
          'Import .env, JSON, or YAML. Directories are walked recursively into folder paths.',
        flags: ['--kind', '--message', '--dry-run', '--yes'],
        examples: ['clavis import ./config/ --dry-run'],
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
        name: 'env diff',
        usage: 'clavis env diff SOURCE TARGET',
        description: 'Compare key names between two environments.',
        examples: ['clavis env diff dev prod'],
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
        flags: ['--endpoint URL', '--secret VALUE', '--provider http'],
        examples: ['clavis syncs create dev --endpoint https://deploy.example.com/hooks/clavis'],
      },
      {
        name: 'syncs run',
        usage: 'clavis syncs run ID [--dry-run] [--yes]',
        description: 'Preview or execute a sync. The first push requires confirmation.',
        flags: ['--dry-run', '--yes'],
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
    title: 'Audit and administration',
    description: 'Verify audit integrity and operate the instance.',
    commands: [
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
        name: 'admin rotate-root-key',
        usage: 'clavis admin rotate-root-key [--init] [--yes] [--config FILE]',
        description:
          'Initialise or rotate the root key offline. Re-wraps org keys and re-encrypts TOTP secrets.',
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
