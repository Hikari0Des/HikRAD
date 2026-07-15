import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import { I18nProvider } from '@hikrad/shared'
import '@hikrad/shared/ui.css'

import { App } from './App'
import { AuthProvider } from './auth/AuthContext'
import { BrandingProvider } from './branding'
import { BrandedManifestLink } from './pwa/BrandedManifestLink'
import { InstallBanner } from './pwa/InstallBanner'
import { OfflineBanner } from './pwa/OfflineBanner'
import { registerServiceWorker } from './pwa/registerServiceWorker'
import { UpdateToast } from './pwa/UpdateToast'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <I18nProvider>
      <BrandingProvider>
        <BrandedManifestLink />
        <OfflineBanner />
        {/* Served under /portal (contract C5) — Vite's base is the basename. */}
        <BrowserRouter basename={import.meta.env.BASE_URL.replace(/\/$/, '')}>
          <AuthProvider>
            <App />
          </AuthProvider>
        </BrowserRouter>
        <InstallBanner />
        <UpdateToast />
      </BrandingProvider>
    </I18nProvider>
  </StrictMode>,
)

registerServiceWorker()
