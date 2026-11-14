/*
 * What the setup guide recommends per platform, and the CI snippets it
 * shows. Two ways to get secrets somewhere:
 *
 *  - push ("sync"): Clavis writes the values into the platform's own secret
 *    store, with a credential for that platform;
 *  - fetch ("CI access"): the job asks Clavis when it runs, with a Clavis
 *    machine token stored as one CI secret.
 */

export type Method = 'push' | 'fetch'

export type Platform = {
  id: string
  name: string
  mark: string
  color: string
  /** Provider id of the push sync, when there is one. */
  sync?: string
  /** CI snippet flavor, when the platform runs jobs that can fetch. */
  ci?: 'github' | 'forgejo' | 'gitlab' | 'shell'
  recommend: Method
  why: string
  /** What the platform can't do that matters for the choice. */
  limits: string[]
  /** Where repository secrets are added, from the repository URL. */
  secretsPage?: (repoUrl: string) => string
}

export const platforms: Platform[] = [
  {
    id: 'forgejo',
    name: 'Forgejo / Gitea Actions',
    mark: 'FJ',
    color: '#fb923c',
    sync: 'forgejo',
    ci: 'forgejo',
    recommend: 'fetch',
    why: 'Forgejo keeps one set of secrets per repository, with no staging/production split. Fetching lets each job ask Clavis for exactly the environment it deploys, under the usual names.',
    limits: [
      'No deployment environments: a repository has one DATABASE_URL, not one per environment.',
      'Two syncs into the same repository overwrite each other’s values.',
      'Every workflow in the repository can read every repository secret.',
    ],
    secretsPage: (repo) => `${repo.replace(/\/+$/, '')}/settings/actions/secrets`,
  },
  {
    id: 'github',
    name: 'GitHub Actions',
    mark: 'GH',
    color: '#8b949e',
    sync: 'github',
    ci: 'github',
    recommend: 'fetch',
    why: 'Values stay in Clavis and every read is audited; rotating a secret needs no re-sync. Pushing into GitHub environment secrets also works well if you prefer GitHub to hold copies.',
    limits: [
      'Pushed values are copies: a change in Clavis reaches GitHub on the next sync run.',
      'Environment secrets need a GitHub environment per Clavis environment.',
    ],
    secretsPage: (repo) => `${repo.replace(/\/+$/, '')}/settings/secrets/actions`,
  },
  {
    id: 'gitlab',
    name: 'GitLab CI',
    mark: 'GL',
    color: '#fc6d26',
    sync: 'gitlab',
    ci: 'gitlab',
    recommend: 'fetch',
    why: 'Clavis pushes GitLab variables without an environment scope, so one project holds one value per name. Fetching gives each job the environment it asks for.',
    limits: ['Pushed variables have no environment scope: one value per name per GitLab project.'],
    secretsPage: (repo) =>
      `${repo.replace(/\/+$/, '')}/-/settings/ci_cd#js-cicd-variables-settings`,
  },
  {
    id: 'vercel',
    name: 'Vercel',
    mark: '▲',
    color: '#ededed',
    sync: 'vercel',
    recommend: 'push',
    why: 'Vercel builds and runs your app with its own environment variables, per target (production, preview, development). Pushing keeps them in step with Clavis.',
    limits: ['Values are copies; run the sync (or schedule it) after changes.'],
  },
  {
    id: 'netlify',
    name: 'Netlify',
    mark: 'N',
    color: '#32e6e2',
    sync: 'netlify',
    recommend: 'push',
    why: 'Netlify reads its own site environment variables at build and run time, so Clavis pushes them there.',
    limits: ['Values are copies; run the sync after changes.'],
  },
  {
    id: 'cloudflare',
    name: 'Cloudflare',
    mark: 'CF',
    color: '#f38020',
    sync: 'cloudflare',
    recommend: 'push',
    why: 'Workers and Pages read secrets from Cloudflare at run time, so Clavis pushes them there.',
    limits: ['One Worker or Pages project per sync.'],
  },
  {
    id: 'aws',
    name: 'AWS',
    mark: 'AWS',
    color: '#ff9900',
    sync: 'aws',
    recommend: 'push',
    why: 'Services on AWS read SSM Parameter Store or Secrets Manager natively, with IAM. Use a path per environment.',
    limits: ['Use a different path prefix per environment.'],
  },
  {
    id: 'gcp',
    name: 'Google Cloud',
    mark: 'GCP',
    color: '#4285f4',
    sync: 'gcp',
    recommend: 'push',
    why: 'Cloud Run, GKE and Functions read Secret Manager natively. Use a name prefix per environment.',
    limits: ['Use a different name prefix per environment.'],
  },
  {
    id: 'vault',
    name: 'HashiCorp Vault',
    mark: 'V',
    color: '#ffd814',
    sync: 'vault',
    recommend: 'push',
    why: 'For apps that already read Vault, while you move to Clavis.',
    limits: ['Use a different path per environment.'],
  },
  {
    id: 'other',
    name: 'Another CI or a server',
    mark: '$_',
    color: '#a3a3a3',
    sync: 'http',
    ci: 'shell',
    recommend: 'fetch',
    why: 'Jenkins, Drone, Woodpecker, Buildkite, cron jobs or a VM: install the clavis CLI and run your command with clavis run. Nothing is copied.',
    limits: ['The machine needs the clavis CLI and network access to Clavis.'],
  },
]

export const methodInfo: Record<Method, { title: string; short: string; how: string[] }> = {
  fetch: {
    title: 'Fetch when the job runs',
    short: 'The job asks Clavis for one environment, with a Clavis token.',
    how: [
      'Clavis creates a machine token that can read only that environment.',
      'You store the token as one CI secret.',
      'Each run fetches the current values; nothing is copied and every read is audited.',
    ],
  },
  push: {
    title: 'Push with a sync',
    short: 'Clavis writes values into the platform, with the platform’s access token.',
    how: [
      'You give Clavis an access token for the platform (saved as a credential).',
      'A sync copies one environment to one place, and you run it after changes.',
      'Apps read the platform’s own secrets as usual.',
    ],
  },
}

/** Release tag of the Clavis action that the snippets use. */
export const actionVersion = 'v0.3.3'

export type ActionSource = 'github' | 'mirror' | 'plain'

export type SnippetInput = {
  ci: NonNullable<Platform['ci']>
  server: string
  fingerprint?: string | undefined
  /** Environment slugs, in the order the user picked them. */
  envs: string[]
  /** CI secret name per environment slug. */
  secretNames: Record<string, string>
  /** Branch that deploys each environment. Empty: deployed only by hand. */
  branches?: Record<string, string> | undefined
  /**
   * Where Forgejo loads the action from: the Clavis repository on GitHub, a
   * mirror on the user's Forgejo, or a copy committed to the repository.
   */
  actionSource?: ActionSource | undefined
  /** Origin of the user's Forgejo, for the mirror ("https://git.example.com"). */
  forgeOrigin?: string | undefined
  /** The runner image already has clavis: skip the download. */
  preinstalled?: boolean | undefined
}

export function secretNameFor(env: string, many: boolean) {
  if (!many) return 'CLAVIS_TOKEN'
  const suffix = env === 'prod' ? 'PRODUCTION' : env.toUpperCase().replace(/[^A-Z0-9]+/g, '_')
  return `CLAVIS_TOKEN_${suffix}`
}

/** The usual branch for an environment slug. */
export function defaultBranch(env: string) {
  if (env === 'prod' || env === 'production') return 'main'
  if (env === 'dev' || env === 'development') return 'develop'
  return env
}

/** Where a Forgejo mirror of the Clavis repository is expected. */
export const mirrorPath = 'mirrors/clavis'

export function actionUses(input: Pick<SnippetInput, 'ci' | 'actionSource' | 'forgeOrigin'>) {
  const path = `integrations/github-action@${actionVersion}`
  if (input.ci !== 'forgejo') return `dumhouse/clavis/${path}`
  if (input.actionSource === 'mirror') {
    const origin = (input.forgeOrigin || 'https://forgejo.example.com').replace(/\/+$/, '')
    return `${origin}/${mirrorPath}/${path}`
  }
  return `https://github.com/dumhouse/clavis/${path}`
}

type Row = { env: string; branch: string; secret: string }

function rowsFor(input: SnippetInput): Row[] {
  return input.envs.map((env) => ({
    env,
    branch: (input.branches?.[env] ?? defaultBranch(env)).trim(),
    secret: input.secretNames[env] ?? 'CLAVIS_TOKEN',
  }))
}

/** A workflow (or job file) that loads the environment its branch deploys. */
export function ciSnippet(input: SnippetInput): string {
  const rows = rowsFor(input)

  if (input.ci === 'gitlab') {
    const jobs = rows.map((row) =>
      [
        `deploy-${row.env}:`,
        `  image: debian:stable-slim`,
        `  rules:`,
        row.branch ? `    - if: $CI_COMMIT_BRANCH == "${row.branch}"` : `    - when: manual`,
        `  variables:`,
        `    CLAVIS_SERVER: ${input.server}`,
        `    CLAVIS_ENV: ${row.env}`,
        ...(input.fingerprint ? [`    CLAVIS_SERVER_FINGERPRINT: "${input.fingerprint}"`] : []),
        `    CLAVIS_TOKEN: $${row.secret}   # masked CI/CD variable`,
        `  script:`,
        ...(input.preinstalled
          ? []
          : [
              `    - apt-get update -qq && apt-get install -yqq curl ca-certificates`,
              `    - curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/main/scripts/install.sh | bash`,
            ]),
        `    - clavis run ./deploy.sh`,
      ].join('\n'),
    )
    return `# .gitlab-ci.yml\n${jobs.join('\n\n')}\n`
  }

  if (input.ci === 'shell') {
    return rows
      .map((row) =>
        [
          `# ${row.env}`,
          `export CLAVIS_SERVER=${input.server}`,
          `export CLAVIS_TOKEN=…   # the ${row.secret} value, from your CI's secret store`,
          ...(input.fingerprint ? [`export CLAVIS_SERVER_FINGERPRINT=${input.fingerprint}`] : []),
          `clavis run --env ${row.env} ./deploy.sh`,
        ].join('\n'),
      )
      .join('\n\n')
  }

  const forgejo = input.ci === 'forgejo'
  const uses = actionUses(input)
  const pushed = rows.filter((row) => row.branch)
  // A run can also be started by hand, picking the environment, when some
  // environment has no branch (or there's more than one environment).
  const dispatch = pushed.length < rows.length || rows.length > 1
  const single = rows.length === 1 && !dispatch

  const trigger = [
    `on:`,
    ...(pushed.length
      ? [`  push:`, `    branches: [${[...new Set(pushed.map((row) => row.branch))].join(', ')}]`]
      : []),
    ...(dispatch
      ? [
          `  workflow_dispatch:   # or run it by hand and pick the environment`,
          `    inputs:`,
          `      environment:`,
          `        type: choice`,
          `        options: [${rows.map((row) => row.env).join(', ')}]`,
        ]
      : []),
  ]

  // Hand-picked environment first, otherwise the branch that was pushed.
  const condition = (row: Row) => {
    const picked = `inputs.environment == '${row.env}'`
    if (!row.branch) return picked
    return `${picked} || (!inputs.environment && github.ref_name == '${row.branch}')`
  }

  const plain = forgejo && input.actionSource === 'plain'
  const steps = rows.flatMap((row) =>
    plain ? plainStep(row, input, single ? '' : condition(row)) : actionStep(row),
  )
  function actionStep(row: Row) {
    return [
      `      - name: Load ${row.env} from Clavis`,
      ...(single ? [] : [`        if: ${condition(row)}`]),
      `        uses: ${uses}`,
      `        with:`,
      `          server: ${input.server}`,
      ...(input.fingerprint ? [`          server-fingerprint: ${input.fingerprint}`] : []),
      `          environment: ${row.env}`,
      `          token: \${{ secrets.${row.secret} }}`,
      ...(input.preinstalled ? [`          install: "false"`] : []),
    ]
  }

  return [
    `# ${forgejo ? '.forgejo/workflows' : '.github/workflows'}/deploy.yaml`,
    ...trigger,
    ``,
    `jobs:`,
    `  deploy:`,
    `    runs-on: ${forgejo ? 'docker' : 'ubuntu-latest'}`,
    `    steps:`,
    `      - uses: actions/checkout@v4`,
    ...(single
      ? []
      : [`      # One load step per environment; only the one matching this run executes.`]),
    ...steps,
    ...(single
      ? []
      : [
          `      - name: Stop when no environment matched`,
          `        if: env.CLAVIS_ENV == ''`,
          `        run: echo "::error::No Clavis environment for \${{ github.ref_name }}" && exit 1`,
        ]),
    `      # Every key of that environment is now an environment variable:`,
    `      - run: ./deploy.sh`,
    ``,
  ].join('\n')
}

/** A load step without the action: the clavis CLI and a few lines of shell. */
function plainStep(row: Row, input: SnippetInput, condition: string) {
  return [
    `      - name: Load ${row.env} from Clavis`,
    ...(condition ? [`        if: ${condition}`] : []),
    `        env:`,
    `          CLAVIS_SERVER: ${input.server}`,
    ...(input.fingerprint ? [`          CLAVIS_SERVER_FINGERPRINT: ${input.fingerprint}`] : []),
    `          CLAVIS_ENV: ${row.env}`,
    `          CLAVIS_TOKEN: \${{ secrets.${row.secret} }}`,
    `        run: |`,
    `          if ! command -v clavis >/dev/null; then   # skip if the runner image has it`,
    `            curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/${actionVersion}/scripts/install.sh \\`,
    `              | CLAVIS_INSTALL_DIR="$RUNNER_TEMP/bin" bash`,
    `            export PATH="$RUNNER_TEMP/bin:$PATH"`,
    `          fi`,
    `          clavis export --format github-env`,
    `          echo "CLAVIS_ENV=$CLAVIS_ENV" >> "$GITHUB_ENV"`,
  ]
}
