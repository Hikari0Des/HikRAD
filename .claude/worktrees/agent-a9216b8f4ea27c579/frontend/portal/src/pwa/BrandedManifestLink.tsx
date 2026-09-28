import { useEffect } from 'react'

import { useBranding } from '../branding'

/**
 * Contract C5: "manifest.webmanifest served from a small endpoint reading
 * branding settings". The build ships a static `/portal/manifest.webmanifest`
 * (generic identity) so the app installs even before/without a backend
 * branding endpoint (NFR-7); once `GET /api/v1/branding` resolves with a
 * non-default name/color, this swaps the `<link rel="manifest">` href to a
 * Blob URL built from it — same approach a server-rendered manifest endpoint
 * would produce, without requiring one.
 */
export function BrandedManifestLink() {
  const branding = useBranding()

  useEffect(() => {
    if (branding.name === 'HikRAD' && !branding.theme_color && !branding.logo_url) return

    const manifest = {
      name: `${branding.name} Portal`,
      short_name: branding.name,
      start_url: '/portal/',
      scope: '/portal/',
      id: '/portal/',
      display: 'standalone',
      background_color: branding.background_color ?? '#f4f6f8',
      theme_color: branding.theme_color ?? '#08748f',
      icons: branding.logo_url
        ? [
            { src: branding.logo_url, sizes: 'any', purpose: 'any' },
            { src: branding.logo_url, sizes: 'any', purpose: 'maskable' },
          ]
        : [
            { src: '/portal/icons/icon.svg', sizes: 'any', type: 'image/svg+xml', purpose: 'any' },
            {
              src: '/portal/icons/icon-maskable.svg',
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
    const previous = link.href
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

    return () => {
      URL.revokeObjectURL(blobUrl)
      if (previous) link.href = previous
    }
  }, [branding])

  return null
}
