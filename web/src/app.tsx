import { RouterProvider } from '@tanstack/react-router'

import { router } from '@/router'
import { useAuth } from '@/lib/auth'
import { OrgProvider } from '@/lib/org'
import { AuthLoading, LoginPage, SetupPage } from '@/pages/auth'
import { JoinPage } from '@/pages/join'
import { ForgotPasswordPage, ResetPasswordPage, VerifyEmailPage } from '@/pages/password-reset'

export function App() {
  const { status } = useAuth()
  if (status === 'loading') return <AuthLoading />
  if (status === 'setup') return <SetupPage />
  if (window.location.pathname === '/join') return <JoinPage />
  if (window.location.pathname === '/forgot') return <ForgotPasswordPage />
  if (window.location.pathname === '/reset') return <ResetPasswordPage />
  if (window.location.pathname === '/verify') return <VerifyEmailPage />
  if (status === 'anonymous') return <LoginPage />
  return (
    <OrgProvider>
      <RouterProvider router={router} />
    </OrgProvider>
  )
}
