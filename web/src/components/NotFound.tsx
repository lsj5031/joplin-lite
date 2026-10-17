import { Link } from '@tanstack/react-router'
import { Mark } from './icons'

export function NotFound({ children }: { children?: any }) {
  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-5 bg-paper-3 px-6 text-center text-ink">
      <Mark width={56} height={56} />
      <p className="label text-ink-soft">joplin-lite / 404</p>
      <h1 className="editor-title">This page is a blank.</h1>
      <div className="max-w-[40ch] text-[14px] text-ink-soft">
        {children || <p>We couldn’t find that page. Your notes are waiting in your workspace.</p>}
      </div>
      <p className="mt-2 flex flex-wrap items-center gap-3">
        <button
          onClick={() => window.history.back()}
          className="min-h-11 rounded-lg border border-line-strong px-4 text-[13px] font-medium hover:bg-paper-2"
        >
          Go back
        </button>
        <Link
          to="/"
          className="primary-button flex min-h-11 items-center rounded-lg px-4 text-[13px] font-medium"
        >
          Open workspace
        </Link>
      </p>
    </main>
  )
}
