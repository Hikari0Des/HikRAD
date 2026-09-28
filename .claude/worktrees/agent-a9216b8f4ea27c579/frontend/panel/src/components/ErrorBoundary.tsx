import { Component, type ReactNode } from 'react'

import { useT } from '@hikrad/shared'

interface FallbackStrings {
  title: string
  body: string
  reload: string
}

interface BoundaryProps {
  strings: FallbackStrings
  children: ReactNode
}

interface BoundaryState {
  hasError: boolean
}

class Boundary extends Component<BoundaryProps, BoundaryState> {
  state: BoundaryState = { hasError: false }

  static getDerivedStateFromError(): BoundaryState {
    return { hasError: true }
  }

  render() {
    if (!this.state.hasError) return this.props.children
    const { title, body, reload } = this.props.strings
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <div role="alert" className="max-w-md rounded-lg bg-surface-raised p-6 text-center shadow">
          <h1 className="text-lg font-semibold">{title}</h1>
          <p className="mt-2 text-sm text-ink-muted">{body}</p>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="mt-4 rounded-md bg-brand px-4 py-2 text-sm text-ink-inverse hover:bg-brand-strong"
          >
            {reload}
          </button>
        </div>
      </div>
    )
  }
}

/** Hook-friendly wrapper: resolves localized strings, the class does the catching. */
export function ErrorBoundary({ children }: { children: ReactNode }) {
  const t = useT()
  return (
    <Boundary
      strings={{
        title: t('errorBoundary.title'),
        body: t('errorBoundary.body'),
        reload: t('errorBoundary.reload'),
      }}
    >
      {children}
    </Boundary>
  )
}
