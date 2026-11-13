import { useState, type ReactNode } from 'react'

import { CertificateNote } from '@/components/certificate-note'
import { CopyCommand } from '@/components/copy-command'
import { cn } from '@/lib/utils'

/*
 * Step-by-step "connect your app" instructions: install the CLI for the
 * reader's OS, sign in, link the folder, run the app. `clavis run` works with
 * any command, so the stack picker only changes the example command.
 */

export type OS = 'macos' | 'windows' | 'linux'

export const operatingSystems: { id: OS; label: string; install: string[] }[] = [
  { id: 'macos', label: 'macOS', install: ['brew install dumhouse/tap/clavis'] },
  {
    id: 'windows',
    label: 'Windows',
    install: [
      'scoop bucket add dumhouse https://github.com/Dumhouse/scoop-bucket',
      'scoop install clavis',
    ],
  },
  {
    id: 'linux',
    label: 'Linux',
    install: [
      'curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/main/scripts/install.sh | bash',
    ],
  },
]

export type Stack = {
  id: string
  label: string
  /** The command after `clavis run`. */
  run: string
  note?: string
}

export const stacks: Stack[] = [
  {
    id: 'node',
    label: 'Node.js',
    run: 'npm run dev',
    note: 'Also pnpm, yarn, bun, Next.js, Vite, NestJS.',
  },
  {
    id: 'python',
    label: 'Python',
    run: 'python app.py',
    note: 'Django: python manage.py runserver. FastAPI: uvicorn main:app --reload.',
  },
  { id: 'go', label: 'Go', run: 'go run .' },
  { id: 'ruby', label: 'Ruby on Rails', run: 'bin/rails server' },
  { id: 'php', label: 'PHP / Laravel', run: 'php artisan serve' },
  {
    id: 'java',
    label: 'Java / Spring',
    run: './mvnw spring-boot:run',
    note: 'Gradle: ./gradlew bootRun.',
  },
  { id: 'dotnet', label: '.NET', run: 'dotnet run' },
  { id: 'rust', label: 'Rust', run: 'cargo run' },
  {
    id: 'compose',
    label: 'Docker Compose',
    run: 'docker compose up',
    note: 'Compose reads the injected variables for ${VAR} substitution.',
  },
  {
    id: 'other',
    label: 'Something else',
    run: '<your command>',
    note: 'Any command works: secrets arrive as environment variables.',
  },
]

const STACK_KEY = 'clavis:stack'
const fallbackStack: Stack = { id: 'node', label: 'Node.js', run: 'npm run dev' }
const fallbackOS = {
  id: 'macos' as OS,
  label: 'macOS',
  install: ['brew install dumhouse/tap/clavis'],
}

export function detectOS(): OS {
  const ua = typeof navigator === 'undefined' ? '' : navigator.userAgent.toLowerCase()
  if (ua.includes('windows')) return 'windows'
  if (ua.includes('mac')) return 'macos'
  return 'linux'
}

export function rememberedStack(): string {
  return (typeof window !== 'undefined' && window.localStorage.getItem(STACK_KEY)) || 'node'
}

export function rememberStack(id: string) {
  window.localStorage.setItem(STACK_KEY, id)
}

function Pills<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: { id: T; label: string }[]
  value: T
  onChange: (id: T) => void
}) {
  return (
    <div role="radiogroup" aria-label={label} className="flex flex-wrap gap-1.5">
      {options.map((option) => (
        <button
          key={option.id}
          type="button"
          role="radio"
          aria-checked={value === option.id}
          className={cn(
            'rounded-app-sm border px-2.5 py-1 text-xs',
            value === option.id
              ? 'border-accent-line bg-accent-soft text-text'
              : 'border-border text-muted hover:bg-hover hover:text-text',
          )}
          onClick={() => {
            onChange(option.id)
          }}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

export function StackPicker({
  value,
  onChange,
}: {
  value: string
  onChange: (id: string) => void
}) {
  return <Pills label="Technology" options={stacks} value={value} onChange={onChange} />
}

export function ConnectGuide({
  orgSlug,
  projectSlug,
  initialStack,
}: {
  orgSlug?: string
  projectSlug?: string
  initialStack?: string
}) {
  const [os, setOS] = useState<OS>(detectOS)
  const [stackId, setStackId] = useState(initialStack ?? rememberedStack)
  const stack: Stack = stacks.find((candidate) => candidate.id === stackId) ?? fallbackStack
  const system = operatingSystems.find((candidate) => candidate.id === os) ?? fallbackOS
  const server = window.location.origin

  const link = ['clavis init']
  if (orgSlug) link.push(`--org ${orgSlug}`)
  if (projectSlug) link.push(`--project ${projectSlug}`)

  const steps: { title: string; text?: string; commands: string[]; after?: ReactNode }[] = [
    { title: 'Install the CLI', commands: system.install },
    {
      title: 'Sign in',
      text: 'Opens your browser to approve this computer.',
      commands: [`clavis login --server ${server}`],
      after: <CertificateNote />,
    },
    {
      title: 'Link your project folder',
      text: 'Run it in the root of your repository. It writes .clavis.yaml (no secrets in it).',
      commands: [link.join(' ')],
    },
    {
      title: 'Run your app with secrets',
      text: `${stack.note ?? 'Secrets arrive as environment variables and are masked in the output.'} Put run: ${stack.run} in .clavis.yaml and a plain clavis run starts it.`,
      commands: [`clavis run ${stack.run}`],
    },
  ]

  return (
    <div className="grid gap-4">
      <div className="grid gap-2 sm:grid-cols-2">
        <div className="grid gap-1.5">
          <span className="text-xs font-medium text-muted">Your computer</span>
          <Pills label="Operating system" options={operatingSystems} value={os} onChange={setOS} />
        </div>
        <div className="grid gap-1.5">
          <span className="text-xs font-medium text-muted">Your app</span>
          <StackPicker
            value={stack.id}
            onChange={(id) => {
              setStackId(id)
              rememberStack(id)
            }}
          />
        </div>
      </div>
      <ol className="grid gap-3">
        {steps.map((step, index) => (
          <li key={step.title} className="grid gap-1.5">
            <div className="flex items-baseline gap-2">
              <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-elev text-[11px] font-semibold text-muted">
                {index + 1}
              </span>
              <span className="text-sm font-medium">{step.title}</span>
            </div>
            {step.text ? <p className="pl-7 text-xs text-muted">{step.text}</p> : null}
            <div className="grid gap-1.5 pl-7">
              {step.commands.map((command) => (
                <CopyCommand key={command} command={command} />
              ))}
              {step.after}
            </div>
          </li>
        ))}
      </ol>
      <p className="text-xs text-dim">
        Other install options and every command are in{' '}
        <a className="underline hover:text-text" href="/docs">
          Docs
        </a>
        . For CI, create a machine token in Identities.
      </p>
    </div>
  )
}
