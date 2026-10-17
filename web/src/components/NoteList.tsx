import type { Note } from '~/lib/mock-data'
import { useVault, type VaultSource } from '~/lib/vault'
import { IconConflict, IconTodo, IconNotebook } from './icons'

type Props = {
  notes: Note[]
  selectedId: string | null
  onSelect: (id: string) => void
  query: string
  onClearQuery: () => void
  listTitle: string
  onOpenNav?: () => void
  hasMore?: boolean
  loading?: boolean
  source: VaultSource
  filtered: boolean
  canWrite: boolean
  onNewNote: () => void
  onRetry: () => void
  onLoadMore?: () => void
}

const shortDate = new Intl.DateTimeFormat('en-GB', { day: '2-digit', month: 'short' })
const fullDate = new Intl.DateTimeFormat('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })
const time = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false })

const fmtWhen = (ts: number) => {
  const d = new Date(ts)
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return (sameYear ? shortDate : fullDate).format(d) + ' ' + time.format(d)
}

export function NoteList({ notes, selectedId, onSelect, query, onClearQuery, listTitle, onOpenNav, hasMore, loading, source, filtered, canWrite, onNewNote, onRetry, onLoadMore }: Props) {
  const { notebookTitle, tagTitle } = useVault()
  return (
    <section className="note-list flex h-full min-w-0 flex-col border-r border-line bg-paper" aria-label="Note list">
      <header className="flex items-center justify-between gap-2 border-b border-line px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          {onOpenNav && (
            <button
              type="button"
              onClick={onOpenNav}
              aria-haspopup="dialog"
              className="min-h-11 shrink-0 rounded-[6px] border border-line-strong px-2 text-[12px] font-medium text-ink transition-colors duration-100 hover:bg-paper-2 lg:hidden"
            >
              Notebooks
            </button>
          )}
          <h2 className="truncate text-[18px] font-semibold tracking-tight text-ink">{listTitle}</h2>
        </div>
        <span className="mono shrink-0 text-[11px] text-ink-soft">
          {notes.length}
          {hasMore ? '+' : ''} shown
        </span>
      </header>

      {notes.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-8 text-center">
          <div className="empty-mark mb-2 text-route" aria-hidden><IconNotebook width={34} height={34} /></div>
          <p className="text-[15px] font-semibold text-ink">
            {source === 'error' ? 'Cannot load your local library' : loading || source === 'loading' ? 'Loading notes…' : query ? `No notes match “${query}”` : filtered ? 'No notes here yet' : 'Your library is ready for its first note'}
          </p>
          <p className="max-w-[36ch] text-[13px] text-ink-soft">
            {source === 'error' ? 'Check that the local backend is running, then retry. Editing is unavailable until it reconnects.' : loading || source === 'loading' ? 'Connecting to your local notes.' : query ? 'Search covers all note titles and bodies. Try a shorter phrase.' : filtered ? 'Create a note or choose another notebook or tag.' : 'Create a note to start writing. Your notes are saved locally.'}
          </p>
          {!loading && source !== 'loading' && <button
            type="button"
            onClick={source === 'error' ? onRetry : query ? onClearQuery : onNewNote}
            disabled={source !== 'error' && !query && !canWrite}
            className="min-h-11 rounded-[6px] border border-line-strong bg-paper-3 px-3 text-[13px] font-medium text-ink transition-colors duration-100 hover:bg-paper-2 disabled:opacity-50"
          >
            {source === 'error' ? 'Retry connection' : query ? 'Clear search' : 'Create a note'}
          </button>}
        </div>
      ) : (
        <ul className="flex-1 overflow-y-auto" onKeyDown={(event) => {
          if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return
          if (!(event.target instanceof HTMLButtonElement) || !event.target.matches('.note-row')) return
          if (!['ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return
          const rows = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('.note-row'))
          const index = rows.indexOf(event.target)
          const next = event.key === 'Home' ? 0 : event.key === 'End' ? rows.length - 1
            : Math.max(0, Math.min(rows.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1)))
          event.preventDefault()
          rows[next].focus({ preventScroll: true })
          rows[next].scrollIntoView({ block: 'nearest', inline: 'nearest' })
        }}>
          {notes.map((n) => {
            const active = n.id === selectedId
            const updated = fmtWhen(n.updatedTime)
            return (
              // Keep every native button searchable and focusable; the browser skips offscreen rendering
              // and remembers measured heights instead of imposing a fixed row height.
              <li key={n.id} style={{ contentVisibility: 'auto', containIntrinsicSize: 'auto 108px' }}>
                <button
                  type="button"
                  onClick={() => onSelect(n.id)}
                  aria-current={active ? 'true' : undefined}
                  className="note-row flex w-full items-start gap-2.5 text-left transition-colors duration-100 hover:bg-paper-2 focus-visible:-outline-offset-2"
                >
                  {(n.isTodo || n.isConflict) && <span className="mt-1 flex w-4 shrink-0 justify-center">
                    {n.isTodo && (
                      <IconTodo
                        width={15}
                        height={15}
                        className={n.todoCompleted ? 'text-moss' : 'text-ink-soft'}
                      />
                    )}
                    {n.isConflict && <IconConflict width={15} height={15} className="text-signal" />}
                  </span>}
                  <span className="min-w-0 flex-1">
                    <span className="mb-1 flex items-center justify-between gap-2">
                      <span className={`truncate text-[14px] ${active ? 'font-semibold text-ink' : 'font-medium text-ink'}`}>{n.title}</span>
                      <time dateTime={new Date(n.updatedTime).toISOString()} title={updated}>{updated.slice(0, 6)}</time>
                    </span>
                    {n.excerpt && (
                      <span className="mb-2 block truncate text-[12px] text-ink-soft">{n.excerpt}</span>
                    )}
                    <span className="block truncate text-[11.5px] text-ink-soft">
                      {notebookTitle(n.notebookId)}
                      {n.tagIds.length > 0 && ` · ${n.tagIds.map(tagTitle).join(' ')}`}
                      {n.isTodo && !n.todoCompleted && n.todoDue && (
                        <span className="text-amber"> · due {fmtWhen(n.todoDue).slice(0, 6)}</span>
                      )}
                    </span>
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
      )}

      {hasMore && onLoadMore && (
        <div className="border-t border-line p-2">
          <button
            type="button"
            onClick={onLoadMore}
            disabled={loading}
            className="w-full rounded-[6px] border border-line-strong bg-paper-3 px-3 py-1.5 text-[12.5px] font-medium text-ink transition-colors duration-100 hover:bg-paper-2 disabled:opacity-50"
          >
            {loading ? 'Loading…' : 'Load more notes'}
          </button>
        </div>
      )}
    </section>
  )
}
