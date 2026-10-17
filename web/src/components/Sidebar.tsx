import * as React from 'react'
import { useVault } from '~/lib/vault'
import { IconNotebook, IconTag } from './icons'

type Props = {
  selectedNotebook: string | null
  onSelectNotebook: (id: string | null) => void
  selectedTag: string | null
  onSelectTag: (id: string | null) => void
}

export function Sidebar({ selectedNotebook, onSelectNotebook, selectedTag, onSelectTag }: Props) {
  const { notebooks, tags, notes, noteCountByNotebook, countsExact, canWrite, activeLoading, createNotebook } = useVault()
  const [creating, setCreating] = React.useState(false)
  const [title, setTitle] = React.useState('')
  const [parentId, setParentId] = React.useState('')
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const disabled = !canWrite || activeLoading || busy
  const allCount = notes.length
  // Per-notebook/tag counts are only meaningful when the loaded rows are the
  // whole library; a summary page must not present page-limited counts as
  // totals.
  const count = (n: number) => (countsExact ? <span className="mono text-[11px] text-ink-soft">{n}</span> : null)

  const row = (active: boolean) =>
    `flex h-8 w-full items-center gap-2 rounded-[6px] px-2 text-left text-[13px] transition-colors duration-100 ${
      active
        ? 'border border-transparent bg-violet-soft font-semibold text-ink'
        : 'border border-transparent text-ink hover:bg-paper-2'
    }`

  const notebookRows = (parent: string | null): React.ReactNode => notebooks.filter((nb) => nb.parentId === parent).map((nb) => (
    <li key={nb.id}>
      <button type="button" aria-current={selectedNotebook === nb.id ? 'page' : undefined} title={nb.title} className={row(selectedNotebook === nb.id)} onClick={() => { onSelectNotebook(nb.id); onSelectTag(null) }}>
        <IconNotebook className="shrink-0 text-ink-soft" width={12} height={12} />
        <span className="flex-1 truncate">{nb.title}</span>
        {count(noteCountByNotebook(nb.id))}
      </button>
      {notebooks.some((child) => child.parentId === nb.id) && <ul className="mt-0.5 ml-2 space-y-0.5 border-l border-line pl-1.5">{notebookRows(nb.id)}</ul>}
    </li>
  ))

  return (
    <nav className="library-nav flex h-full flex-col gap-5 overflow-y-auto bg-paper-2 px-3 py-4" aria-label="Notebooks and tags">
      <div>
        <div className="flex items-center justify-between">
          <h2 className="label px-2 pb-1.5 text-ink-soft">Notebooks</h2>
          <button type="button" disabled={disabled} aria-expanded={creating} className="min-h-10 rounded px-2 text-[12px] text-ink disabled:opacity-50" onClick={() => {
            setParentId(selectedNotebook ?? '')
            setCreating(!creating)
          }}>New notebook</button>
        </div>
        {creating && <form aria-label="Create notebook" className="mb-3 rounded-[6px] border border-line-strong p-2" onSubmit={async (event) => {
          event.preventDefault()
          if (disabled || !title.trim()) return
          setBusy(true)
          setError(null)
          try {
            await createNotebook(title.trim(), parentId || null)
            setTitle('')
            setCreating(false)
          } catch (e) { setError(e instanceof Error ? e.message : String(e)) }
          finally { setBusy(false) }
        }}>
          <fieldset disabled={disabled} className="space-y-2 disabled:opacity-50">
            <label className="block text-[12px] text-ink">Notebook name
              <input required value={title} onChange={(e) => setTitle(e.target.value)} className="mt-1 min-h-10 w-full rounded border border-line-strong bg-paper px-2" />
            </label>
            <label className="block text-[12px] text-ink">Parent notebook
              <select aria-label="Parent notebook" value={parentId} onChange={(e) => setParentId(e.target.value)} className="mt-1 min-h-10 w-full rounded border border-line-strong bg-paper px-2">
                <option value="">Top level</option>
                {notebooks.map((nb) => <option key={nb.id} value={nb.id}>{nb.title}</option>)}
              </select>
            </label>
            <div className="flex gap-2">
              <button type="submit" disabled={!title.trim()} className="primary-button min-h-10 rounded px-2 text-[12px] font-semibold disabled:opacity-50">Create notebook</button>
              <button type="button" onClick={() => setCreating(false)} className="min-h-10 px-2 text-[12px] text-ink">Cancel</button>
            </div>
          </fieldset>
        </form>}
        {error && <p role="alert" className="px-2 pb-2 text-[12px] text-signal">{error}</p>}
        <ul className="space-y-0.5">
          <li>
            <button type="button" aria-current={selectedNotebook === null && selectedTag === null ? 'page' : undefined} className={row(selectedNotebook === null && selectedTag === null)} onClick={() => { onSelectNotebook(null); onSelectTag(null) }}>
              <IconNotebook className="shrink-0 text-ink-soft" />
              <span className="flex-1 truncate">All notes</span>
              <span className="mono text-[11px] text-ink-soft">{allCount}{countsExact ? '' : '+'}</span>
            </button>
          </li>
          {notebookRows(null)}
        </ul>
      </div>

      <div>
        <h2 className="label px-2 pb-1.5 text-ink-soft">Tags</h2>
        <ul className="space-y-0.5">
          {tags.map((t) => {
            const noteCount = notes.filter((n) => n.tagIds.includes(t.id)).length
            return (
              <li key={t.id}>
                <button type="button" aria-current={selectedTag === t.id ? 'page' : undefined} className={row(selectedTag === t.id)} onClick={() => { onSelectTag(selectedTag === t.id ? null : t.id); onSelectNotebook(null) }}>
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
