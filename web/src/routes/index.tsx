import * as React from 'react'
import { createFileRoute } from '@tanstack/react-router'
import {
  notebooks,
  notes as mockNotes,
  notebookTitle,
  tagTitle,
  type Note,
  type SyncState,
  type VaultState,
} from '~/lib/mock-data'
import { TopBar } from '~/components/TopBar'
import { Sidebar } from '~/components/Sidebar'
import { NoteList } from '~/components/NoteList'
import { EditorPane } from '~/components/EditorPane'
import { StatusStrip } from '~/components/StatusStrip'

export const Route = createFileRoute('/')({
  component: Workspace,
})

function Workspace() {
  const [notes, setNotes] = React.useState<Note[]>(mockNotes)
  const [selectedId, setSelectedId] = React.useState<string | null>('3f2a91c4e5b84d17a6c0f2b9d8e45a11')
  const [query, setQuery] = React.useState('')
  const [notebookFilter, setNotebookFilter] = React.useState<string | null>(null)
  const [tagFilter, setTagFilter] = React.useState<string | null>(null)
  const [vault, setVault] = React.useState<VaultState>('unlocked')
  const [syncState, setSyncState] = React.useState<SyncState>('synced')
  const [lastSync, setLastSync] = React.useState<number>(Date.now() - 4 * 60_000)
  const [navOpen, setNavOpen] = React.useState(false)
  const [mobileView, setMobileView] = React.useState<'list' | 'edit'>('list')
  const searchRef = React.useRef<HTMLInputElement | null>(null)

  // ⌘K / Ctrl+K focuses search; Esc clears it
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        searchRef.current?.focus()
      }
      if (e.key === 'Escape') setQuery('')
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const runSync = () => {
    setSyncState('syncing')
    window.setTimeout(() => {
      setLastSync(Date.now())
      setSyncState(notes.some((n) => n.isConflict) ? 'conflict' : 'synced')
    }, 2400)
  }

  const q = query.trim().toLowerCase()
  const filtered = notes.filter((n) => {
    if (notebookFilter) {
      const nb = notebooks.find((b) => b.id === notebookFilter)
      const scope = [notebookFilter, ...notebooks.filter((b) => b.parentId === notebookFilter).map((b) => b.id)]
      if (!scope.includes(n.notebookId) && nb) return false
    }
    if (tagFilter && !n.tagIds.includes(tagFilter)) return false
    if (!q) return true
    const haystack = [
      n.title,
      n.body,
      notebookTitle(n.notebookId),
      ...n.tagIds.map(tagTitle),
    ]
      .join(' ')
      .toLowerCase()
    return haystack.includes(q)
  })

  const selected = notes.find((n) => n.id === selectedId) ?? null

  const selectNote = (id: string) => {
    setSelectedId(id)
    setMobileView('edit')
  }

  const listTitle = q
    ? `Search “${query.trim()}”`
    : tagFilter
      ? `Tag: ${tagTitle(tagFilter)}`
      : notebookFilter
        ? notebookTitle(notebookFilter)
        : 'All notes'

  const createNote = () => {
    const id = crypto.randomUUID().replace(/-/g, '').slice(0, 32)
    const now = Date.now()
    const fresh: Note = {
      id,
      title: 'Untitled note',
      body: '# Untitled note\n\n',
      notebookId: notebookFilter ?? 'nb-inbox',
      tagIds: [],
      createdTime: now,
      updatedTime: now,
      isTodo: false,
      todoCompleted: false,
      todoDue: null,
      isConflict: false,
      conflictOriginalTitle: null,
    }
    setNotes((prev) => [fresh, ...prev])
    setSelectedId(id)
    setMobileView('edit')
  }

  const toggleTodo = (id: string) =>
    setNotes((prev) =>
      prev.map((n) => (n.id === id ? { ...n, todoCompleted: !n.todoCompleted, updatedTime: Date.now() } : n)),
    )

  const changeBody = (id: string, body: string) =>
    setNotes((prev) => prev.map((n) => (n.id === id ? { ...n, body, updatedTime: Date.now() } : n)))

  return (
    <div className="flex h-dvh flex-col">
      <TopBar
        query={query}
        onQuery={setQuery}
        syncState={syncState}
        onSync={runSync}
        vault={vault}
        onToggleVault={() => setVault((v) => (v === 'unlocked' ? 'locked' : 'unlocked'))}
        onNewNote={createNote}
        onPreviewState={setSyncState}
        searchRef={searchRef}
      />

      <div className="grid min-h-0 flex-1 lg:grid-cols-[248px_360px_minmax(0,1fr)]">
        {/* sidebar: slide-over below lg */}
        <div
          className={`${navOpen ? 'fixed inset-y-0 left-0 z-30 w-[260px] shadow-[0_0_24px_rgba(15,29,45,0.25)]' : 'hidden'} lg:static lg:z-auto lg:block lg:w-auto lg:shadow-none`}
        >
          <Sidebar
            selectedNotebook={notebookFilter}
            onSelectNotebook={(id) => {
              setNotebookFilter(id)
              setNavOpen(false)
              setMobileView('list')
            }}
            selectedTag={tagFilter}
            onSelectTag={(id) => {
              setTagFilter(id)
              setNavOpen(false)
              setMobileView('list')
            }}
          />
        </div>
        {navOpen && (
          <button
            type="button"
            aria-label="Close navigation"
            className="fixed inset-0 z-20 bg-navy/30 lg:hidden"
            onClick={() => setNavOpen(false)}
          />
        )}

        <div className={`min-h-0 min-w-0 ${mobileView === 'edit' ? 'hidden lg:block' : 'block'}`}>
          <NoteList
            notes={filtered}
            selectedId={selectedId}
            onSelect={selectNote}
            query={q}
            onClearQuery={() => setQuery('')}
            listTitle={listTitle}
            onOpenNav={() => setNavOpen(true)}
          />
        </div>

        <div className={`min-h-0 min-w-0 ${mobileView === 'list' ? 'hidden lg:block' : 'block'}`}>
          <EditorPane
            note={selected}
            vault={vault}
            onUnlock={() => setVault('unlocked')}
            onToggleTodo={toggleTodo}
            onChangeBody={changeBody}
            onBack={() => setMobileView('list')}
          />
        </div>
      </div>

      <StatusStrip
        state={syncState}
        lastSync={lastSync}
        noteCount={notes.length}
        notebookCount={notebooks.length}
      />
    </div>
  )
}
