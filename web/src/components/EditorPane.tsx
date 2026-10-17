import * as React from 'react'
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'
import type { Note, VaultState } from '~/lib/mock-data'
import { useVault } from '~/lib/vault'
import { IconBack, IconConflict, IconLock, IconTodo } from './icons'

const md = new MarkdownIt({ linkify: true })

type Props = {
  note: Note | null
  /** loading is true while the selected note's body is being fetched. */
  loading?: boolean
  vault: VaultState
  onUnlock: () => void
  onToggleTodo: (id: string) => void
  onChangeBody: (id: string, body: string) => void
  onBack: () => void
}

export function EditorPane({ note, loading, vault, onUnlock, onToggleTodo, onChangeBody, onBack }: Props) {
  const { notebookTitle, tagTitle } = useVault()
  const [tab, setTab] = React.useState<'write' | 'preview'>('write')
  const [html, setHtml] = React.useState('')

  React.useEffect(() => {
    if (tab === 'preview' && note) {
      setHtml(DOMPurify.sanitize(md.render(note.body)))
    }
  }, [tab, note])

  if (!note) {
    return (
      <section className="flex h-full flex-1 items-center justify-center bg-paper-3">
        <p className="text-[14px] text-ink-soft">
          {loading ? 'Loading note…' : 'Select a note to start reading.'}
        </p>
      </section>
    )
  }

  const locked = vault === 'locked'

  return (
    <section className="relative flex h-full min-w-0 flex-1 flex-col bg-paper-3" aria-label="Note editor">
      <header className="border-b border-line px-6 pt-3.5">
        <div className="flex items-start gap-2">
          <button
            type="button"
            onClick={onBack}
            className="mt-1 rounded-[6px] border border-line p-1 text-ink-soft transition-colors duration-100 hover:bg-paper-2 lg:hidden"
            aria-label="Back to note list"
          >
            <IconBack width={16} height={16} />
          </button>
          <h1 className="flex-1 truncate text-[20px] leading-tight font-semibold tracking-[-0.015em] text-ink">
            {note.title}
          </h1>
          {note.isTodo && (
            <button
              type="button"
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
          <span className="mono text-[11px] text-ink-soft">{notebookTitle(note.notebookId)}</span>
          <span className="mono text-[11px] text-ink-soft">·</span>
          <span className="mono text-[11px] text-ink-soft">
            updated {new Date(note.updatedTime).toLocaleString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false })}
          </span>
          <span className="mono text-[11px] text-ink-soft">
            id {note.id.slice(0, 4)}…{note.id.slice(-4)}
          </span>
          {note.tagIds.map((t) => (
            <span key={t} className="rounded-full border border-line-strong bg-paper px-2 py-0.5 text-[11px] text-ink-soft">
              {tagTitle(t)}
            </span>
          ))}
        </div>
        <div className="flex gap-1 pb-0">
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
        </div>
      </header>

      {note.isConflict && (
        <div className="flex items-start gap-2.5 border-b border-line bg-signal/[0.07] px-6 py-2.5">
          <IconConflict width={16} height={16} className="mt-0.5 shrink-0 text-signal" />
          <div className="flex-1">
            <p className="text-[12.5px] font-semibold text-signal">
              Both this device and Joplin desktop changed this note — nothing was overwritten.
            </p>
            <p className="text-[12px] text-ink-soft">
              Your version is kept as “{note.conflictOriginalTitle ?? note.title} (Conflict)” in this notebook.
            </p>
          </div>
          <div className="flex shrink-0 gap-1.5">
            <button type="button" className="rounded-[6px] border border-signal/40 bg-paper px-2.5 py-1 text-[12px] font-medium text-signal transition-colors duration-100 hover:bg-signal/10">
              Open my copy
            </button>
            <button type="button" className="rounded-[6px] border border-line-strong bg-paper px-2.5 py-1 text-[12px] font-medium text-ink transition-colors duration-100 hover:bg-paper-2">
              Keep synced version
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
          onChange={(e) => onChangeBody(note.id, e.target.value)}
          spellCheck={false}
          aria-label="Note body, markdown source"
          className="mono flex-1 resize-none bg-transparent px-6 py-4 text-[13px] leading-[1.7] text-ink outline-none"
        />
      ) : (
        <div className="flex-1 overflow-y-auto px-6 py-4">
          <div className="prose-note" dangerouslySetInnerHTML={{ __html: html }} />
        </div>
      )}

      {locked && (
        <div className="absolute inset-0 flex items-center justify-center bg-paper/95 backdrop-blur-[2px]">
          <div className="flex w-[320px] flex-col items-center gap-3 text-center">
            <IconLock width={28} height={28} className="text-amber" />
            <p className="text-[16px] font-semibold text-ink">Vault locked</p>
            <p className="text-[12.5px] text-ink-soft">
              Note contents are encrypted at sync time. Enter the master password to decrypt this note.
            </p>
            <input
              type="password"
              placeholder="Master password"
              aria-label="Master password"
              className="w-full rounded-[6px] border border-line-strong bg-paper px-3 py-1.5 text-[13px] text-ink outline-none focus:border-route"
            />
            <button
              type="button"
              onClick={onUnlock}
              className="w-full rounded-[6px] bg-route px-3 py-1.5 text-[13px] font-semibold text-paper transition-opacity duration-100 hover:opacity-90"
            >
              Unlock vault
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
