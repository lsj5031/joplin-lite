import type { Note } from '~/lib/mock-data'
import { notebookTitle, tagTitle } from '~/lib/mock-data'
import { IconConflict, IconTodo } from './icons'

type Props = {
  notes: Note[]
  selectedId: string | null
  onSelect: (id: string) => void
  query: string
  onClearQuery: () => void
  listTitle: string
  onOpenNav?: () => void
}

const fmtWhen = (ts: number) => {
  const d = new Date(ts)
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return d.toLocaleDateString('en-GB', {
    day: '2-digit',
    month: 'short',
    ...(sameYear ? {} : { year: 'numeric' }),
  }) + ' ' + d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false })
}

export function NoteList({ notes, selectedId, onSelect, query, onClearQuery, listTitle, onOpenNav }: Props) {
  return (
    <section className="flex h-full min-w-0 flex-col border-r border-line bg-paper" aria-label="Note list">
      <header className="flex items-baseline justify-between gap-2 border-b border-line px-4 py-2.5">
        <div className="flex min-w-0 items-baseline gap-2">
          {onOpenNav && (
            <button
              type="button"
              onClick={onOpenNav}
              className="shrink-0 rounded-[6px] border border-line-strong px-2 py-0.5 text-[11.5px] font-medium text-ink transition-colors duration-100 hover:bg-paper-2 lg:hidden"
            >
              Notebooks
            </button>
          )}
          <h2 className="truncate text-[13px] font-semibold text-ink">{listTitle}</h2>
        </div>
        <span className="mono shrink-0 text-[11px] text-ink-soft">{notes.length} shown</span>
      </header>

      {notes.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-8 text-center">
          <p className="text-[15px] font-semibold text-ink">No notes match {query ? `“${query}”` : 'this filter'}</p>
          <p className="max-w-[36ch] text-[13px] text-ink-soft">
            Search looks at titles, note bodies, notebooks, and tags. Try a shorter phrase.
          </p>
          <button
            type="button"
            onClick={onClearQuery}
            className="rounded-[6px] border border-line-strong bg-paper-3 px-3 py-1.5 text-[13px] font-medium text-ink transition-colors duration-100 hover:bg-paper-2"
          >
            Clear search
          </button>
        </div>
      ) : (
        <ul className="flex-1 overflow-y-auto">
          {notes.map((n) => {
            const active = n.id === selectedId
            return (
              <li key={n.id}>
                <button
                  type="button"
                  onClick={() => onSelect(n.id)}
                  aria-current={active ? 'true' : undefined}
                  className={`flex w-full items-center gap-2.5 border-b border-line px-3.5 py-2 text-left transition-colors duration-100 ${
                    active ? 'border-l border-l-violet bg-violet-soft' : 'border-l border-l-transparent hover:bg-paper-2'
                  }`}
                >
                  <span className="flex w-4 shrink-0 justify-center">
                    {n.isTodo && (
                      <IconTodo
                        width={15}
                        height={15}
                        className={n.todoCompleted ? 'text-moss' : 'text-ink-soft'}
                      />
                    )}
                    {n.isConflict && <IconConflict width={15} height={15} className="text-signal" />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className={`block truncate text-[13.5px] ${active ? 'font-semibold text-ink' : 'font-medium text-ink'}`}>
                      {n.title}
                    </span>
                    <span className="block truncate text-[11.5px] text-ink-soft">
                      {notebookTitle(n.notebookId)}
                      {n.tagIds.length > 0 && ` · ${n.tagIds.map(tagTitle).join(' ')}`}
                      {n.isTodo && !n.todoCompleted && n.todoDue && (
                        <span className="text-amber"> · due {fmtWhen(n.todoDue).slice(0, 6)}</span>
                      )}
                    </span>
                  </span>
                  <span className="mono shrink-0 text-[11px] text-ink-soft">{fmtWhen(n.updatedTime)}</span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
