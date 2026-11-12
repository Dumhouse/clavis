import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Link,
} from '@tanstack/react-router'

import { AppShell } from '@/components/app-shell'
import { ProjectsPage } from '@/pages/projects'
import { SecretsPage } from '@/pages/secrets'
import { SectionPage } from '@/pages/section'

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

const sections = ['/shared-sets'] as const

const snapshotsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/snapshots',
  component: lazyRouteComponent(() => import('@/pages/snapshots'), 'SnapshotsPage'),
})

const healthRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/health',
  component: lazyRouteComponent(() => import('@/pages/health'), 'HealthPage'),
})

const requestsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/requests',
  component: lazyRouteComponent(() => import('@/pages/requests'), 'RequestsPage'),
})

const accessRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/access',
  component: lazyRouteComponent(() => import('@/pages/access'), 'ProjectAccessPage'),
})

const environmentsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/environments',
  component: lazyRouteComponent(() => import('@/pages/environments'), 'EnvironmentsPage'),
})

const schemaRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/schema',
  component: lazyRouteComponent(() => import('@/pages/schema'), 'SchemaPage'),
})

const promoteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/promote',
  component: lazyRouteComponent(() => import('@/pages/promote'), 'PromotePage'),
})

const dynamicRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/dynamic',
  component: lazyRouteComponent(() => import('@/pages/dynamic'), 'DynamicPage'),
})

const rotationRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/projects/$project/rotation',
  component: lazyRouteComponent(() => import('@/pages/rotation'), 'RotationPage'),
})

const deviceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/device',
  component: lazyRouteComponent(() => import('@/pages/device'), 'DevicePage'),
})

const auditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/audit',
  component: lazyRouteComponent(() => import('@/pages/audit'), 'AuditPage'),
})

const identitiesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/identities',
  component: lazyRouteComponent(() => import('@/pages/identities'), 'IdentitiesPage'),
})

const accessMatrixRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/access',
  component: lazyRouteComponent(() => import('@/pages/access-matrix'), 'AccessMatrixPage'),
})

const membersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/members',
  component: lazyRouteComponent(() => import('@/pages/members'), 'MembersPage'),
})

const integrationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/integrations',
  component: lazyRouteComponent(() => import('@/pages/integrations'), 'IntegrationsPage'),
})

const setsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sets',
  component: lazyRouteComponent(() => import('@/pages/sets'), 'SharedSetsPage'),
})

const docsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/docs',
  component: lazyRouteComponent(() => import('@/pages/docs'), 'DocsPage'),
})

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: lazyRouteComponent(() => import('@/pages/settings'), 'OrgSettingsPage'),
})

const accountRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/account',
  component: lazyRouteComponent(() => import('@/pages/settings'), 'AccountSettingsPage'),
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
  healthRoute,
  requestsRoute,
  accessRoute,
  environmentsRoute,
  schemaRoute,
  promoteRoute,
  dynamicRoute,
  rotationRoute,
  deviceRoute,
  accessMatrixRoute,
  membersRoute,
  identitiesRoute,
  auditRoute,
  integrationsRoute,
  setsRoute,
  docsRoute,
  settingsRoute,
  accountRoute,
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
      <p className="font-mono text-sm text-dim">404</p>
      <h1 className="font-head text-2xl font-semibold tracking-tight">Page not found</h1>
      <p className="text-base text-muted">The page you asked for does not exist.</p>
      <Link
        to="/"
        className="rounded-app-sm bg-accent px-3.5 py-2 text-sm font-medium text-accent-fg hover:opacity-90"
      >
        Back to projects
      </Link>
    </div>
  )
}
