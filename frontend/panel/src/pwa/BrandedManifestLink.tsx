import { useEffect } from 'react'

interface Branding {
  name: string
  logo_url: string | null
  theme_color: string | null
  background_color: string | null
}

/**
 * Contract C5: swaps `<link rel="manifest">` to a per-ISP manifest once
 * `GET /api/v1/branding` resolves (mirrors frontend/portal/src/pwa/
 * BrandedManifestLink.tsx). Self-contained fetch (no dependency on panel's
 * own API client) so this stays entirely inside the Phase-4 src/pwa/**
 * exception scope.
 */
export function BrandedManifestLink() {
  useEffect(() => {
    let cancelled = false
    fetch('/api/v1/branding', { headers: { Accept: 'application/json' } })
      .then((res) => (res.ok ? (res.json() as Promise<Branding>) : null))
      .then((branding) => {
        if (cancelled || !branding) return
        if (branding.name === 'HikRAD' && !branding.theme_color && !branding.logo_url) return
        apply(branding)
      })
      .catch(() => {
        // Offline-first (NFR-7): keep the static fallback manifest.
      })
    return () => {
      cancelled = true
    }
  }, [])

  return null
}

function apply(branding: Branding) {
  const manifest = {
    name: `${branding.name} Panel`,
    short_name: branding.name,
    start_url: '/',
    scope: '/',
    id: '/',
    display: 'standalone',
    background_color: branding.background_color ?? '#f4f6f8',
    theme_color: branding.theme_color ?? '#08748f',
    icons: branding.logo_url
      ? [
          { src: branding.logo_url, sizes: 'any', purpose: 'any' },
          { src: branding.logo_url, sizes: 'any', purpose: 'maskable' },
        ]
      : [
          { src: '/icons/icon.svg', sizes: 'any', type: 'image/svg+xml', purpose: 'any' },
          {
            src: '/icons/icon-maskable.svg',
            sizes: 'any',
            type: 'image/svg+xml',
            purpose: 'maskable',
          },
        ],
  }
  const blobUrl = URL.createObjectURL(
    new Blob([JSON.stringify(manifest)], { type: 'application/manifest+json' }),
  )
  let link = document.querySelector<HTMLLinkElement>('link[rel="manifest"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'manifest'
    document.head.appendChild(link)
  }
  link.href = blobUrl

  if (branding.theme_color) {
    let meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    if (!meta) {
      meta = document.createElement('meta')
      meta.name = 'theme-color'
      document.head.appendChild(meta)
    }
    meta.content = branding.theme_color
  }
}
