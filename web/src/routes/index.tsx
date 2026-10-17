import * as React from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { VaultProvider, useVault } from '~/lib/vault'
import { TopBar } from '~/components/TopBar'
import { Sidebar } from '~/components/Sidebar'
import { NoteList } from '~/components/NoteList'
import { EditorPane } from '~/components/EditorPane'
import { StatusStrip } from '~/components/StatusStrip'

export const Route = createFileRoute('/')({ component: WorkspaceRoute })

function WorkspaceRoute() {
  return (
    <VaultProvider>
      <Workspace />
    </VaultProvider>
  )
}

function Workspace() {
  const vault = useVault()
  const { notes, notebooks, notebookTitle, tagTitle, source, activeNote, listLoading, hasMore } = vault
  const [selectedId, setSelectedId] = React.useState<string | null>(null)
  const [query, setQuery] = React.useState('')
  const [notebookFilter, setNotebookFilter] = React.useState<string | null>(null)
  const [tagFilter, setTagFilter] = React.useState<string | null>(null)
  const [actionError, setActionError] = React.useState<string | null>(null)
  const [reloading, setReloading] = React.useState(false)
  const [mobileView, setMobileView] = React.useState<'list' | 'edit'>('list')
  const searchRef = React.useRef<HTMLInputElement | null>(null)
  const navRef = React.useRef<HTMLDialogElement>(null)

  const reportError = (error: unknown) => setActionError(error instanceof Error ? error.message : String(error))

  const changeQuery = (value: string) => {
    setQuery(value)
    if (value.trim()) {
      setNotebookFilter(null)
      setTagFilter(null)
      setMobileView('list')
    }
  }

  // ⌘K / Ctrl+K focuses search; Esc clears it
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (document.querySelector('dialog[open]')) return
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        searchRef.current?.focus()
      }
      if (e.key === 'Escape') setQuery('')
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // The backend owns filtering and search. Debounce typing so a keystroke
  // does not turn into a request per character; filters apply immediately.
  React.useEffect(() => {
    const delay = query.trim() ? 250 : 0
    const t = window.setTimeout(() => {
      void vault.loadList({
        query: query.trim(),
        notebookId: notebookFilter,
        tagId: tagFilter,
      }).catch(reportError)
    }, delay)
    return () => window.clearTimeout(t)
  }, [query, notebookFilter, tagFilter, vault.loadList])

  // Keep a valid selection as the list loads/changes, and load its body once.
  React.useEffect(() => {
    if (selectedId && notes.some((n) => n.id === selectedId)) return
    setSelectedId(notes[0]?.id ?? null)
  }, [notes, selectedId])
  React.useEffect(() => {
    if (!selectedId) return
    void vault.selectNote(selectedId).catch(reportError)
  }, [selectedId, vault.selectNote])

  const reload = async () => {
    setReloading(true)
    setActionError(null)
    try {
      await vault.reload()
    } catch (error) {
      reportError(error)
    } finally {
      setReloading(false)
    }
  }

  const q = query.trim()
  const selected = activeNote

  const selectNote = (id: string) => {
    setSelectedId(id)
    setMobileView('edit')
  }

  const listTitle = q
    ? `All notes · “${q}”`
    : tagFilter
      ? `Tag: ${tagTitle(tagFilter)}`
      : notebookFilter
        ? notebookTitle(notebookFilter)
        : 'All notes'

  const createNote = async () => {
    setActionError(null)
    try {
      const id = await vault.createNote({ notebookId: notebookFilter ?? '' })
      setQuery('')
      setTagFilter(null)
      setSelectedId(id)
      setMobileView('edit')
    } catch (error) {
      reportError(error)
    }
  }

  const toggleTodo = (id: string) => {
    void vault.toggleTodo(id).catch(reportError)
  }

  const changeBody = (id: string, body: string) => {
    void vault.patchNote(id, { body }).catch(reportError)
  }

  const sidebar = <Sidebar
    selectedNotebook={notebookFilter}
    onSelectNotebook={(id) => {
      setQuery('')
      setNotebookFilter(id)
      navRef.current?.close()
      setMobileView('list')
    }}
    selectedTag={tagFilter}
    onSelectTag={(id) => {
      setQuery('')
      setTagFilter(id)
      navRef.current?.close()
      setMobileView('list')
    }}
  />

  return (
    <div className="flex h-dvh flex-col">
      <TopBar
        query={query}
        onQuery={changeQuery}
        source={source}
        canWrite={vault.canWrite}
        reloading={reloading}
        onReload={() => void reload()}
        onNewNote={() => void createNote()}
        searchRef={searchRef}
      />

      <div className="workspace-grid grid min-h-0 flex-1">
        <div className="hidden min-h-0 lg:block">{sidebar}</div>
        <dialog ref={navRef} aria-label="Notebooks and tags" onClick={(event) => { if (event.target === event.currentTarget) navRef.current?.close() }} className="fixed inset-y-0 left-0 m-0 h-dvh max-h-none w-[min(300px,85vw)] max-w-none bg-paper-2 p-0 text-ink shadow-xl backdrop:bg-navy/40">
          <div className="flex h-full flex-col">
            <div className="flex shrink-0 items-center justify-between border-b border-line px-3 py-2">
              <h2 className="text-[14px] font-semibold">Navigation</h2>
              <button type="button" autoFocus onClick={() => navRef.current?.close()} className="min-h-11 rounded-[6px] border border-line-strong px-3 text-[13px]">Close navigation</button>
            </div>
            <div className="min-h-0 flex-1">{sidebar}</div>
          </div>
        </dialog>

        <div className={`min-h-0 min-w-0 ${mobileView === 'edit' ? 'hidden lg:block' : 'block'}`}>
          <NoteList
            notes={notes}
            selectedId={selectedId}
            onSelect={selectNote}
            query={q}
            onClearQuery={() => setQuery('')}
            listTitle={listTitle}
            onOpenNav={() => navRef.current?.showModal()}
            hasMore={hasMore}
            loading={listLoading}
            source={source}
            filtered={!!notebookFilter || !!tagFilter}
            canWrite={vault.canWrite}
            onNewNote={() => void createNote()}
            onRetry={() => void reload()}
            onLoadMore={() => void vault.loadMore().catch(reportError)}
          />
        </div>

        <div className={`min-h-0 min-w-0 ${mobileView === 'list' ? 'hidden lg:block' : 'block'}`}>
          <EditorPane
            note={selected}
            loading={vault.activeLoading}
            onToggleTodo={toggleTodo}
            onChangeBody={changeBody}
            onBack={() => setMobileView('list')}
          />
        </div>
      </div>

      <StatusStrip
        noteCount={notes.length}
        notebookCount={notebooks.length}
        hasMore={hasMore}
        source={source}
        saveState={vault.saveState}
        error={actionError ?? vault.error}
        reloading={reloading}
        onRetry={() => void reload()}
      />
    </div>
  )
}
