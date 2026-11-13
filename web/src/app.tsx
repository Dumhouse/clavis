import { RouterProvider } from '@tanstack/react-router'

import { router } from '@/router'
import { useAuth } from '@/lib/auth'
import { OrgProvider } from '@/lib/org'
import { AuthLoading, LoginPage, SetupPage } from '@/pages/auth'
import { JoinPage } from '@/pages/join'
import { ForgotPasswordPage, ResetPasswordPage, VerifyEmailPage } from '@/pages/password-reset'
import { SharePage } from '@/pages/share'
import { WelcomePage } from '@/pages/welcome'
import { ApprovalPage } from '@/pages/approval'

export function App() {
  const { status, onboarding } = useAuth()
  if (window.location.pathname.startsWith('/share/')) return <SharePage />
  if (window.location.pathname === '/approvals') return <ApprovalPage />
  if (status === 'loading') return <AuthLoading />
  if (status === 'setup') return <SetupPage />
  if (window.location.pathname === '/join') return <JoinPage />
  if (window.location.pathname === '/forgot') return <ForgotPasswordPage />
  if (window.location.pathname === '/reset') return <ResetPasswordPage />
  if (window.location.pathname === '/verify') return <VerifyEmailPage />
  if (status === 'anonymous') return <LoginPage />
  if (onboarding) return <WelcomePage />
  return (
    <OrgProvider>
      <RouterProvider router={router} />
    </OrgProvider>
  )
}
