import { ArrowRight, Loader2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import {
  ConnectGuide,
  rememberStack,
  rememberedStack,
  StackPicker,
} from '@/components/connect-guide'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/field'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth } from '@/lib/auth'
import { AuthLayout, Field } from '@/pages/auth'

/*
 * Shown once, right after first-run setup: name the organization, create the
 * first project, then connect an app. Every step after the first can be
 * skipped; the app works the same either way.
 */

type Step = 'org' | 'project' | 'connect'

function slugify(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
}

function emailCompany(email: string | undefined): string {
  const domain = email?.split('@')[1]?.split('.')[0] ?? ''
  const generic = [
    'gmail',
    'googlemail',
    'outlook',
    'hotmail',
    'yahoo',
    'icloud',
    'proton',
    'protonmail',
    'example',
  ]
  if (!domain || generic.includes(domain)) return ''
  return domain.charAt(0).toUpperCase() + domain.slice(1)
}

export function WelcomePage() {
  const { user, orgs, setOrgs, finishOnboarding } = useAuth()
  const org = orgs[0]
  const firstName = user?.name.split(' ')[0] ?? ''
  const [step, setStep] = useState<Step>('org')
  const [orgName, setOrgName] = useState(() => emailCompany(user?.email))
  const [orgSlug, setOrgSlug] = useState(org?.slug ?? '')
  const [projectName, setProjectName] = useState('')
  const [projectSlug, setProjectSlug] = useState<string | undefined>()
  const [stack, setStack] = useState(rememberedStack)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function finish(target: string) {
    finishOnboarding()
    window.location.assign(target)
  }

  async function saveOrg(event: FormEvent) {
    event.preventDefault()
    if (!org) return
    setBusy(true)
    setError(null)
    const name = orgName.trim()
    const slug = slugify(name) || org.slug
    const result = await api.PATCH('/api/v1/orgs/{org}', {
      params: { path: { org: org.slug } },
      body: { name, slug },
    })
    setBusy(false)
    if (result.error || !result.data) {
      setError(apiErrorMessage(result.error, 'Could not rename the organization.'))
      return
    }
    setOrgSlug(result.data.slug)
    setOrgs(
      orgs.map((candidate) =>
        candidate.id === org.id
          ? { ...candidate, name: result.data.name, slug: result.data.slug }
          : candidate,
      ),
    )
    setStep('project')
  }

  async function createProject(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const name = projectName.trim()
    const result = await api.POST('/api/v1/orgs/{org}/projects', {
      params: { path: { org: orgSlug } },
      body: { name, slug: slugify(name) },
    })
    setBusy(false)
    if (result.error || !result.data) {
      setError(apiErrorMessage(result.error, 'Could not create the project.'))
      return
    }
    rememberStack(stack)
    setProjectSlug(result.data.slug)
    setStep('connect')
  }

  const errorBox = error ? (
    <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
  ) : null

  const progress = (
    <ol className="mb-4 flex items-center gap-2 text-2xs text-dim" aria-label="Progress">
      {(['org', 'project', 'connect'] as const).map((id, index) => (
        <li key={id} className={step === id ? 'font-semibold text-text' : undefined}>
          {index + 1}.{' '}
          {id === 'org' ? 'Organization' : id === 'project' ? 'First project' : 'Connect your app'}
        </li>
      ))}
    </ol>
  )

  if (step === 'org') {
    return (
      <AuthLayout
        title={firstName ? `Welcome, ${firstName}` : 'Welcome'}
        subtitle="Your account is ready. Two quick questions and you can store your first secret."
      >
        {progress}
        <form
          className="space-y-3"
          onSubmit={(event) => {
            void saveOrg(event)
          }}
        >
          <Field label="Organization name" hint="your company, team, or just your name">
            <Input
              required
              autoFocus
              maxLength={100}
              placeholder="Acme"
              value={orgName}
              onChange={(event) => {
                setOrgName(event.target.value)
              }}
            />
          </Field>
          <p className="text-xs text-muted">
            An organization holds projects, members and audit logs. You can rename it later in
            Settings.
          </p>
          {errorBox}
          <Button
            type="submit"
            variant="primary"
            size="lg"
            className="w-full"
            disabled={busy || orgName.trim() === ''}
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : null}
            Continue
            <ArrowRight className="size-4" />
          </Button>
        </form>
      </AuthLayout>
    )
  }

  if (step === 'project') {
    return (
      <AuthLayout
        title="Your first project"
        subtitle="A project is usually one app or service. It gets Development, Staging and Production environments."
        wide
      >
        {progress}
        <form
          className="space-y-4"
          onSubmit={(event) => {
            void createProject(event)
          }}
        >
          <Field label="Project name">
            <Input
              required
              autoFocus
              maxLength={100}
              placeholder="Web app"
              value={projectName}
              onChange={(event) => {
                setProjectName(event.target.value)
              }}
            />
          </Field>
          <div className="grid gap-1.5">
            <span className="text-xs font-medium text-muted">What is it built with?</span>
            <StackPicker value={stack} onChange={setStack} />
            <p className="text-2xs text-dim">
              Only used to show the right commands. Clavis works with any language.
            </p>
          </div>
          {errorBox}
          <div className="flex gap-2">
            <Button
              type="button"
              variant="secondary"
              size="lg"
              onClick={() => {
                finish('/')
              }}
            >
              Skip for now
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="flex-1"
              disabled={busy || projectName.trim() === ''}
            >
              {busy ? <Loader2 className="size-4 animate-spin" /> : null}
              Create project
              <ArrowRight className="size-4" />
            </Button>
          </div>
        </form>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout
      title="Connect your app"
      subtitle="Run these on your computer. Then add secrets in the web app and your app receives them as environment variables."
      wide
    >
      {progress}
      <ConnectGuide
        orgSlug={orgSlug}
        {...(projectSlug ? { projectSlug } : {})}
        initialStack={stack}
      />
      <div className="mt-5 flex gap-2">
        <Button
          type="button"
          variant="primary"
          size="lg"
          className="flex-1"
          onClick={() => {
            finish(projectSlug ? `/projects/${projectSlug}` : '/')
          }}
        >
          Add your first secret
          <ArrowRight className="size-4" />
        </Button>
      </div>
    </AuthLayout>
  )
}
