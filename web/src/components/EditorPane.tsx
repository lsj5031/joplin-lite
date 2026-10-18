import * as React from 'react'
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'
import type { Note } from '~/lib/mock-data'
import { useVault } from '~/lib/vault'
import { IconBack, IconConflict, IconTodo, Mark } from './icons'

const md = new MarkdownIt({ linkify: true })
// Links leave the app: the desktop shell opens a new-window request in the
// system browser, and the explicit target keeps the same behavior in a tab.
md.renderer.rules.link_open = (tokens, idx, options, _env, self) => {
  const token = tokens[idx]
  const href = String(token.attrGet('href') ?? '')
  if (/^https?:\/\//i.test(href)) {
    token.attrSet('target', '_blank')
    token.attrSet('rel', 'noopener noreferrer')
  }
  return self.renderToken(tokens, idx, options)
}
// Only list-item text becomes a task checkbox; raw HTML remains disabled and
// the complete rendered document still passes through DOMPurify.
md.core.ruler.after('inline', 'task_lists', (state) => {
  state.tokens.forEach((token, index) => {
    if (token.type !== 'inline' || state.tokens[index - 1]?.type !== 'paragraph_open' || state.tokens[index - 2]?.type !== 'list_item_open') return
    const first = token.children?.[0]
    const match = first?.type === 'text' && /^\[([ xX])\]\s+/.exec(first.content)
    if (!match || !first) return
    first.content = first.content.slice(match[0].length)
    const checkbox = new state.Token('html_inline', '', 0)
    checkbox.content = `<input type="checkbox" disabled aria-label="${match[1] === ' ' ? 'Incomplete task' : 'Completed task'}"${match[1] === ' ' ? '' : ' checked'}> `
    token.children!.unshift(checkbox)
  })
})

type Props = {
  note: Note | null
  /** loading is true while the selected note's body is being fetched. */
  loading?: boolean
  onToggleTodo: (id: string) => void
  onChangeBody: (id: string, body: string) => void
  onOpenNote: (id: string) => void
  onBack: () => void
}

export function EditorPane({ note, loading, onToggleTodo, onChangeBody, onOpenNote, onBack }: Props) {
  const { notebookTitle, tagTitle, notebooks, tags, source, canWrite, patchNote, deleteNote, createTag, setNoteTags, error: vaultError } = useVault()
  const [tab, setTab] = React.useState<'write' | 'preview'>('preview')
  const [managing, setManaging] = React.useState(false)
  const [title, setTitle] = React.useState('')
  const [notebookId, setNotebookId] = React.useState('')
  const [tagIds, setTagIds] = React.useState<string[]>([])
  const [newTag, setNewTag] = React.useState('')
  const [confirmDelete, setConfirmDelete] = React.useState(false)
  const [acknowledgedConflict, setAcknowledgedConflict] = React.useState<string | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const disabled = !!loading || !canWrite || busy

  React.useEffect(() => {
    // Notes open as rendered pages; Write is a deliberate switch.
    setTab('preview')
    setManaging(false)
    setError(null)
    setNewTag('')
    setConfirmDelete(false)
  }, [note?.id])

  function handlePreviewClick(event: React.MouseEvent<HTMLDivElement>) {
    const anchor = (event.target as HTMLElement | null)?.closest('a')
    const href = anchor?.getAttribute('href') ?? ''
    if (!/^https?:\/\//i.test(href)) return
    event.preventDefault()
    window.open(href, '_blank', 'noopener,noreferrer')
  }

  const preview = React.useRef<{ id: string; body: string; html: string } | null>(null)
  // Keep only this note's last preview across tab changes; edits discard it even in Write.
  if (preview.current && (preview.current.id !== note?.id || preview.current.body !== note?.body)) {
    preview.current = null
  }
  let html = ''
  if (tab === 'preview' && !loading && note) {
    preview.current ??= { id: note.id, body: note.body, html: DOMPurify.sanitize(md.render(note.body)) }
    html = preview.current.html
  }

  async function mutate(action: () => Promise<void>) {
    setBusy(true)
    setError(null)
    try { await action() } catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setBusy(false) }
  }

  if (!note) {
    return (
      <section className="flex h-full flex-1 flex-col items-center justify-center gap-4 bg-paper-3 px-8 text-center" aria-label="Note editor">
        <Mark width={56} height={56} />
        <h1 className="editor-title">{source === 'error' ? 'A moment to reconnect.' : 'A little room to think.'}</h1>
        <p className="max-w-[32ch] text-[14px] text-ink-soft">{source === 'error' ? 'Reconnect to your local database to read or create notes.' : loading || source === 'loading' ? 'Opening your workspace…' : 'Select a note, or start a new page. Your next idea belongs here.'}</p>
      </section>
    )
  }

  return (
    <section className="editor-pane relative flex h-full min-w-0 flex-1 flex-col bg-paper-3" aria-label="Note editor">
      <header className="editor-header border-b border-line">
        <div className="flex items-start gap-2">
          <button
            type="button"
            onClick={onBack}
            className="flex min-h-11 min-w-11 items-center justify-center rounded-lg border border-line text-ink-soft transition-colors duration-100 hover:bg-paper-2 lg:hidden"
            aria-label="Back to note list"
          >
            <IconBack width={16} height={16} />
          </button>
          <h1 className="editor-title min-w-0 flex-1 truncate leading-tight text-ink" title={note.title}>
            {note.title}
          </h1>
          {note.isTodo && (
            <button
              type="button"
              disabled={disabled}
              onClick={() => onToggleTodo(note.id)}
              className={`flex items-center gap-1.5 rounded-[6px] border px-2.5 py-1 text-[12px] font-medium transition-colors duration-100 ${
                note.todoCompleted
                  ? 'border-moss bg-moss/10 text-moss'
                  : 'border-line-strong bg-paper text-ink hover:bg-paper-2'
              }`}
            >
              <IconTodo width={14} height={14} />
              {note.todoCompleted ? 'Completed' : 'Mark complete'}
            </button>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2">
          <span className="text-[12px] text-ink-soft">{notebookTitle(note.notebookId)}</span>
          <span className="text-[11px] text-ink-soft">/</span>
          <span className="text-[12px] text-ink-soft">
            updated {new Date(note.updatedTime).toLocaleString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false })}
          </span>
          {note.tagIds.map((t) => (
            <span key={t} className="rounded-full border border-line-strong bg-paper px-2 py-0.5 text-[11px] text-ink-soft">
              {tagTitle(t)}
            </span>
          ))}
        </div>
        <div className="editor-tabs flex gap-1">
          {(['write', 'preview'] as const).map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => setTab(t)}
              aria-pressed={tab === t}
              className={`-mb-px border-b-2 px-3 py-1.5 text-[12.5px] font-medium capitalize transition-colors duration-100 ${
                tab === t
                  ? 'border-b-violet text-ink'
                  : 'border-b-transparent text-ink-soft hover:text-ink'
              }`}
            >
              {t}
            </button>
          ))}
          <button type="button" disabled={disabled} aria-expanded={managing} onClick={() => {
            setTitle(note.title)
            setNotebookId(note.notebookId)
            setTagIds(note.tagIds)
            setConfirmDelete(false)
            setManaging(!managing)
          }} className="ml-auto min-h-10 rounded px-3 text-[12.5px] font-medium text-ink disabled:opacity-50">
            Manage note
          </button>
        </div>
      </header>

      {managing && (
        <form aria-label="Manage note" className="max-h-[50vh] shrink-0 overflow-y-auto border-b border-line bg-paper-2 px-6 py-3" onSubmit={(event) => {
          event.preventDefault()
          if (disabled || !title.trim()) return
          void mutate(async () => {
            await patchNote(note.id, { title: title.trim(), notebookId })
            let selectedTags = tagIds
            if (newTag.trim()) {
              const existing = tags.find((tag) => tag.title.toLocaleLowerCase() === newTag.trim().toLocaleLowerCase())
              const id = existing?.id ?? await createTag(newTag.trim())
              selectedTags = [...new Set([...selectedTags, id])]
              setTagIds(selectedTags)
              setNewTag('')
            }
            await setNoteTags(note.id, selectedTags)
            setManaging(false)
          })
        }}>
          <fieldset disabled={disabled} className="space-y-3 disabled:opacity-50">
            <label className="block text-[12px] text-ink">Title
              <input required value={title} onChange={(e) => setTitle(e.target.value)} className="mt-1 block min-h-10 w-full rounded-[6px] border border-line-strong bg-paper px-2 text-[14px]" />
            </label>
            <label className="block text-[12px] text-ink">Notebook
              <select aria-label="Notebook" value={notebookId} onChange={(e) => setNotebookId(e.target.value)} className="mt-1 block min-h-10 w-full rounded-[6px] border border-line-strong bg-paper px-2 text-[13px]">
                <option value="">No notebook</option>
                {notebooks.map((nb) => <option key={nb.id} value={nb.id}>{nb.title}</option>)}
              </select>
            </label>
            <fieldset>
              <legend className="text-[12px] text-ink">Tags</legend>
              <div className="flex max-h-28 flex-wrap gap-x-4 overflow-y-auto">
                {tags.map((tag) => <label key={tag.id} className="flex min-h-10 items-center gap-2 text-[12px] text-ink">
                  <input type="checkbox" checked={tagIds.includes(tag.id)} onChange={(e) => setTagIds(e.target.checked ? [...tagIds, tag.id] : tagIds.filter((id) => id !== tag.id))} />
                  {tag.title}
                </label>)}
              </div>
            </fieldset>
            <label className="block text-[12px] text-ink">New tag (optional)
              <input value={newTag} onChange={(e) => setNewTag(e.target.value)} className="mt-1 block min-h-10 w-full rounded-[6px] border border-line-strong bg-paper px-2 text-[13px]" />
            </label>
            <div className="flex flex-wrap gap-2">
              <button type="submit" disabled={!title.trim()} className="primary-button min-h-10 rounded-lg px-3 text-[13px] font-semibold disabled:opacity-50">Save details</button>
              <button type="button" onClick={() => setManaging(false)} className="min-h-10 rounded-[6px] border border-line-strong px-3 text-[13px]">Cancel</button>
              <button type="button" className="ml-auto min-h-10 rounded-[6px] border border-signal/40 px-3 text-[13px] text-signal" onClick={() => setConfirmDelete(true)}>Delete note</button>
            </div>
            {confirmDelete && <div role="group" aria-label="Confirm note deletion" className="rounded-[6px] border border-signal/40 p-3">
              <p role="alert" className="mb-2 text-[13px] text-ink">Delete “{note.title}”? This cannot be undone.</p>
              <div className="flex flex-wrap gap-2">
                <button type="button" className="min-h-10 rounded-lg bg-[#b93630] px-3 text-[13px] font-semibold text-white" onClick={() => void mutate(() => deleteNote(note.id))}>Delete permanently</button>
                <button type="button" className="min-h-10 rounded-[6px] border border-line-strong px-3 text-[13px] text-ink" onClick={() => setConfirmDelete(false)}>Keep note</button>
              </div>
            </div>}
          </fieldset>
        </form>
      )}
      {(error || vaultError) && <p role="alert" className="border-b border-line px-6 py-2 text-[12px] text-signal">{error || vaultError}</p>}
      {!canWrite && <p role="status" className="border-b border-line px-6 py-2 text-[12px] text-ink-soft">Editing unavailable. Reconnect to your backend to make changes.</p>}

      {note.isConflict && acknowledgedConflict !== note.id && (
        <div className="flex items-start gap-2.5 border-b border-line bg-signal/[0.07] px-6 py-2.5">
          <IconConflict width={16} height={16} className="mt-0.5 shrink-0 text-signal" />
          <div className="flex-1">
            <p className="text-[12.5px] font-semibold text-signal">
              Another device changed this note at the same time — nothing was overwritten.
            </p>
            <p className="text-[12px] text-ink-soft">
              This note is your kept copy. The synced version is still filed as the original note.
            </p>
          </div>
          <div className="flex shrink-0 gap-1.5">
            <button
              type="button"
              disabled={!note.conflictOriginalId}
              onClick={() => { if (note.conflictOriginalId) onOpenNote(note.conflictOriginalId) }}
              className="rounded-[6px] border border-signal/40 bg-paper px-2.5 py-1 text-[12px] font-medium text-signal transition-colors duration-100 hover:bg-signal/10 disabled:opacity-50"
            >
              Open synced version
            </button>
            <button
              type="button"
              onClick={() => setAcknowledgedConflict(note.id)}
              className="rounded-[6px] border border-line-strong bg-paper px-2.5 py-1 text-[12px] font-medium text-ink transition-colors duration-100 hover:bg-paper-2"
            >
              Keep my copy
            </button>
          </div>
        </div>
      )}

      {loading && (
        <div className="mono border-b border-line bg-paper-2 px-6 py-1.5 text-[11px] text-ink-soft">
          Loading note body…
        </div>
      )}

      {tab === 'write' ? (
        <textarea
          value={note.body}
          disabled={disabled}
          onChange={(e) => onChangeBody(note.id, e.target.value)}
          spellCheck={false}
          aria-label="Note body, markdown source"
          className="editor-body mono flex-1 resize-none bg-transparent text-ink outline-none"
        />
      ) : (
        <div className="editor-body flex-1 overflow-y-auto" onClick={handlePreviewClick} onAuxClick={handlePreviewClick}>
          <div className="prose-note" dangerouslySetInnerHTML={{ __html: html }} />
        </div>
      )}

    </section>
  )
}
