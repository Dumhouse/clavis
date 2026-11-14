import '@fontsource-variable/geist'
import '@fontsource-variable/manrope'
// Optional fonts (Settings → Appearance). Only @font-face rules are added
// here; a font file is downloaded only when the font is chosen.
import '@fontsource-variable/inter'
import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/500.css'
import '@fontsource/ibm-plex-sans/600.css'
import '@fontsource/ibm-plex-sans/700.css'
import '@fontsource-variable/jetbrains-mono'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import './styles.css'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from '@/app'
import { ThemeProvider } from '@/components/theme-provider'
import { DialogHost } from '@/components/ui/dialogs'
import { ToastHost } from '@/components/ui/toast'
import { AuthProvider } from '@/lib/auth'
import { applyFonts } from '@/lib/fonts'

applyFonts()

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

const root = document.getElementById('root')
if (!root) throw new Error('missing #root element')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthProvider>
          <App />
          <DialogHost />
          <ToastHost />
        </AuthProvider>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
)
