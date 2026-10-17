import type { VaultSource } from '~/lib/vault'

type Props = {
  noteCount: number
  notebookCount: number
  hasMore?: boolean
  source: VaultSource
  saveState: 'saved' | 'saving' | 'error'
  error?: string | null
  reloading: boolean
  onRetry: () => void
}

export function StatusStrip({ noteCount, notebookCount, hasMore, source, saveState, error, reloading, onRetry }: Props) {
  const failed = source === 'error' || saveState === 'error' || !!error
  const status = source === 'mock' ? 'Demo · changes are temporary'
    : source === 'loading' ? 'Connecting to local database…'
    : saveState === 'error' ? 'Local save failed · changes need retry'
    : source === 'error' ? 'Local database disconnected · editing unavailable'
    : error ? 'Local action failed · retry required'
    : saveState === 'saving' ? 'Saving locally…'
    : 'Saved locally'

  return (
    <footer className="workspace-status flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-t border-line bg-paper-3 px-4 py-2 text-[11px] text-ink-soft md:px-6">
      <div role="status" className="min-w-0 flex-1">
        <p className={`flex items-center gap-2 ${failed ? 'text-[12px] text-signal' : 'text-ink-soft'}`}>
          {!failed && source === 'live' && <span aria-hidden className={`status-dot ${saveState === 'saving' ? 'opacity-40' : ''}`} />}{status}
        </p>
        {error && <p className="break-words text-[11px] text-signal">{error}</p>}
      </div>
      {!failed && source === 'live' && <span className="hidden text-[11px] text-ink-soft sm:inline">{noteCount}{hasMore ? '+' : ''} shown · {notebookCount} notebooks</span>}
      {failed && <button type="button" onClick={onRetry} disabled={reloading} className="min-h-11 shrink-0 rounded-lg border border-line-strong px-3 font-medium hover:bg-paper-2 disabled:opacity-50">{reloading ? 'Retrying…' : 'Retry'}</button>}
    </footer>
  )
}
