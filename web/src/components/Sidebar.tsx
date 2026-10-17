import { useVault } from '~/lib/vault'
import { IconChevron, IconNotebook, IconTag } from './icons'

type Props = {
  selectedNotebook: string | null
  onSelectNotebook: (id: string | null) => void
  selectedTag: string | null
  onSelectTag: (id: string | null) => void
}

export function Sidebar({ selectedNotebook, onSelectNotebook, selectedTag, onSelectTag }: Props) {
  const { notebooks, tags, notes, noteCountByNotebook, countsExact } = useVault()
  const roots = notebooks.filter((n) => n.parentId === null)
  const allCount = notes.length
  // Per-notebook/tag counts are only meaningful when the loaded rows are the
  // whole library; a summary page must not present page-limited counts as
  // totals.
  const count = (n: number) => (countsExact ? <span className="mono text-[11px] text-ink-soft">{n}</span> : null)

  const row = (active: boolean) =>
    `flex h-8 w-full items-center gap-2 rounded-[6px] px-2 text-left text-[13px] transition-colors duration-100 ${
      active
        ? 'border border-violet bg-violet-soft font-semibold text-ink'
        : 'border border-transparent text-ink hover:bg-paper-2'
    }`

  return (
    <nav className="flex h-full flex-col gap-5 overflow-y-auto bg-paper-2 px-3 py-4" aria-label="Notebooks and tags">
      <div>
        <h2 className="label px-2 pb-1.5 text-ink-soft">Notebooks</h2>
        <ul className="space-y-0.5">
          <li>
            <button type="button" className={row(selectedNotebook === null && selectedTag === null)} onClick={() => { onSelectNotebook(null); onSelectTag(null) }}>
              <IconNotebook className="shrink-0 text-ink-soft" />
              <span className="flex-1 truncate">All notes</span>
              <span className="mono text-[11px] text-ink-soft">{allCount}{countsExact ? '' : '+'}</span>
            </button>
          </li>
          {roots.map((nb) => {
            const children = notebooks.filter((c) => c.parentId === nb.id)
            return (
              <li key={nb.id}>
                <button type="button" className={row(selectedNotebook === nb.id)} onClick={() => { onSelectNotebook(nb.id); onSelectTag(null) }}>
                  {children.length > 0 ? (
                    <IconChevron className={`shrink-0 text-ink-soft ${selectedNotebook === nb.id ? 'rotate-90' : ''} transition-transform duration-100`} width={12} height={12} />
                  ) : (
                    <span className="w-3 shrink-0" />
                  )}
                  <span className="flex-1 truncate">{nb.title}</span>
                  {count(noteCountByNotebook(nb.id))}
                </button>
                {children.length > 0 && (
                  <ul className="mt-0.5 ml-[15px] space-y-0.5 border-l border-line pl-1.5">
                    {children.map((c) => (
                      <li key={c.id}>
                        <button type="button" className={row(selectedNotebook === c.id)} onClick={() => { onSelectNotebook(c.id); onSelectTag(null) }}>
                          <span className="flex-1 truncate">{c.title}</span>
                          {count(noteCountByNotebook(c.id))}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </li>
            )
          })}
        </ul>
      </div>

      <div>
        <h2 className="label px-2 pb-1.5 text-ink-soft">Tags</h2>
        <ul className="space-y-0.5">
          {tags.map((t) => {
            const noteCount = notes.filter((n) => n.tagIds.includes(t.id)).length
            return (
              <li key={t.id}>
                <button type="button" className={row(selectedTag === t.id)} onClick={() => { onSelectTag(selectedTag === t.id ? null : t.id); onSelectNotebook(null) }}>
                  <IconTag className="shrink-0 text-ink-soft" />
                  <span className="flex-1 truncate">{t.title}</span>
                  {count(noteCount)}
                </button>
              </li>
            )
          })}
        </ul>
      </div>
    </nav>
  )
}
