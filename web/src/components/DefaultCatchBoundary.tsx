import {
  ErrorComponent,
  Link,
  useLocation,
  useRouter,
} from '@tanstack/react-router'
import type { ErrorComponentProps } from '@tanstack/react-router'
import { Mark } from './icons'

export function DefaultCatchBoundary({ error }: ErrorComponentProps) {
  const router = useRouter()
  const isRoot = useLocation({
    select: (location) => location.pathname === '/',
  })

  console.error('DefaultCatchBoundary Error:', error)

  return (
    <main className="flex min-h-dvh min-w-0 flex-col items-center justify-center gap-5 bg-paper-3 px-6 text-center text-ink">
      <Mark width={56} height={56} />
      <h1 className="editor-title">Let’s try that again.</h1>
      <p className="max-w-[40ch] text-[14px] text-ink-soft">Something interrupted this page. Try again to reopen your workspace.</p>
      <details className="max-w-full overflow-auto rounded-lg border border-line p-3 text-left text-[12px] text-ink-soft">
        <summary className="cursor-pointer">Error details</summary>
        <ErrorComponent error={error} />
      </details>
      <div className="flex gap-2 items-center flex-wrap">
        <button
          onClick={() => {
            router.invalidate()
          }}
          className="primary-button min-h-11 rounded-lg px-4 text-[13px] font-medium"
        >
          Try again
        </button>
        {isRoot ? (
          <Link
            to="/"
            className="flex min-h-11 items-center rounded-lg border border-line-strong px-4 text-[13px] font-medium"
          >
            Home
          </Link>
        ) : (
          <Link
            to="/"
            className="flex min-h-11 items-center rounded-lg border border-line-strong px-4 text-[13px] font-medium"
            onClick={(e) => {
              e.preventDefault()
              window.history.back()
            }}
          >
            Go Back
          </Link>
        )}
      </div>
    </main>
  )
}
