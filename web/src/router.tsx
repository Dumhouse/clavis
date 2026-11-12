import { createRootRoute, createRoute, createRouter, Link } from '@tanstack/react-router'

import { AppShell } from '@/components/app-shell'
import { AuditPage } from '@/pages/audit'
import { ProjectsPage } from '@/pages/projects'
import { DevicePage } from '@/pages/device'
import { EnvironmentsPage } from '@/pages/environments'
import { IdentitiesPage } from '@/pages/identities'
import { MembersPage } from '@/pages/members'
import { ProjectAccessPage } from '@/pages/access'
import { AccessMatrixPage } from '@/pages/access-matrix'
import { SecretsPage } from '@/pages/secrets'
import { SnapshotsPage } from '@/pages/snapshots'
import { SectionPage } from '@/pages/section'
import { SettingsPage } from '@/pages/settings'

const rootRoute = createRootRoute({
  component: AppShell,
  notFoundComponent: NotFound,
})

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: ProjectsPage,
})

const projectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project',
  component: SecretsPage,
})

const sections = ['/shared-sets', '/health', '/requests'] as const

const snapshotsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/snapshots',
  component: SnapshotsPage,
})

const accessRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/access',
  component: ProjectAccessPage,
})

const environmentsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/environments',
  component: EnvironmentsPage,
})

const deviceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/device',
  component: DevicePage,
})

const auditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/audit',
  component: AuditPage,
})

const identitiesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/identities',
  component: IdentitiesPage,
})

const accessMatrixRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/access',
  component: AccessMatrixPage,
})

const membersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/members',
  component: MembersPage,
})

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsPage,
})

const sectionRoutes = sections.map((path) =>
  createRoute({
    getParentRoute: () => rootRoute,
    path,
    component: () => <SectionPage path={path} />,
  }),
)

const routeTree = rootRoute.addChildren([
  indexRoute,
  projectRoute,
  snapshotsRoute,
  accessRoute,
  environmentsRoute,
  deviceRoute,
  accessMatrixRoute,
  membersRoute,
  identitiesRoute,
  auditRoute,
  settingsRoute,
  ...sectionRoutes,
])

export const router = createRouter({
  routeTree,
  defaultPreload: 'intent',
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

function NotFound() {
  return (
    <div className="flex flex-col items-start gap-3">
      <p className="font-mono text-[12px] text-dim">404</p>
      <h1 className="font-head text-[24px] font-semibold tracking-tight">Page not found</h1>
      <p className="text-[13px] text-muted">The page you asked for does not exist.</p>
      <Link
        to="/"
        className="rounded-app-sm bg-accent px-3.5 py-2 text-[12px] font-medium text-accent-fg hover:opacity-90"
      >
        Back to projects
      </Link>
    </div>
  )
}
