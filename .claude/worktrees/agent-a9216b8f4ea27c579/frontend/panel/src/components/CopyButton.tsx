import { useState } from 'react'

import { useT } from '@hikrad/shared'

import { Button } from './Button'

/** Copy-to-clipboard button with a transient "copied" confirmation (FR-14.1). */
export function CopyButton({
  text,
  size = 'sm',
  className,
}: {
  text: string
  size?: 'sm' | 'md'
  className?: string
}) {
  const t = useT()
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // Clipboard blocked (insecure context / permission): fall back to a
      // hidden textarea + execCommand so copy still works over plain HTTP.
      const ta = document.createElement('textarea')
      ta.value = text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      try {
        document.execCommand('copy')
      } catch {
        /* give up silently */
      }
      document.body.removeChild(ta)
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Button variant="secondary" size={size} className={className} onClick={copy}>
      {copied ? t('ui.copied') : t('ui.copy')}
    </Button>
  )
}
