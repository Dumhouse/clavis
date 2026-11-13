import { defineAsyncComponent, h } from 'vue'
import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'

import DownloadMatrix from './components/DownloadMatrix.vue'
import InstallWizard from './components/InstallWizard.vue'
import Landing from './components/Landing.vue'
import NavStars from './components/NavStars.vue'
import NotFound from './components/NotFound.vue'
import './custom.css'
import './mock.css'
import './reference.css'

/*
 * Click any screenshot in a docs page to view it full size. A tiny lightbox
 * keeps the docs free of extra dependencies.
 */
function installLightbox() {
  if (typeof window === 'undefined') return
  document.addEventListener('click', (event) => {
    const target = event.target as HTMLElement | null
    if (!(target instanceof HTMLImageElement) || !target.closest('.vp-doc')) return
    if (target.closest('a')) return

    const overlay = document.createElement('div')
    overlay.className = 'lightbox'
    overlay.setAttribute('role', 'dialog')
    overlay.setAttribute('aria-label', target.alt || 'Image preview')
    const img = document.createElement('img')
    img.src = target.currentSrc || target.src
    img.alt = target.alt
    overlay.appendChild(img)

    const close = () => {
      overlay.classList.remove('open')
      document.removeEventListener('keydown', onKey)
      setTimeout(() => overlay.remove(), 180)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close()
    overlay.addEventListener('click', close)
    document.addEventListener('keydown', onKey)
    document.body.appendChild(overlay)
    requestAnimationFrame(() => overlay.classList.add('open'))
  })
}

export default {
  extends: DefaultTheme,
  Layout: () => h(DefaultTheme.Layout, null, { 'nav-bar-content-after': () => h(NavStars), 'not-found': () => h(NotFound) }),
  enhanceApp({ app }) {
    app.component('Landing', Landing)
    app.component('DownloadMatrix', DownloadMatrix)
    app.component('InstallWizard', InstallWizard)
    // Loaded on demand: it carries the whole CLI reference JSON.
    app.component('CliSearch', defineAsyncComponent(() => import('./components/CliSearch.vue')))
    installLightbox()
  },
} satisfies Theme
