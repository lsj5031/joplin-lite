import * as React from 'react'
import { createFileRoute } from '@tanstack/react-router'
import type { Note, SyncState, VaultState } from '~/lib/mock-data'
import { VaultProvider, useVault } from '~/lib/vault'
import { TopBar } from '~/components/TopBar'
import { Sidebar } from '~/components/Sidebar'
import { NoteList } from '~/components/NoteList'
import { EditorPane } from '~/components/EditorPane'
import { StatusStrip } from '~/components/StatusStrip'

export const Route = createFileRoute('/')({
  // Prototype state preview via URL (?state=conflict|locked|...) — same states
  // as the settings popover buttons, made addressable so docs screenshots are
  // reproducible.
  validateSearch: (search: Record<string, unknown>): { state?: string } => ({
    state: typeof search.state === 'string' ? search.state : undefined,
  }),
  component: WorkspaceRoute,
})

function WorkspaceRoute() {
  return (
    <VaultProvider>
      <Workspace />
    </VaultProvider>
  )
}

const SYNC_STATES = ['synced', 'syncing', 'conflict', 'error', 'locked'] as const
const isSyncState = (s: string | undefined): s is SyncState =>
  SYNC_STATES.includes(s as SyncState)

function Workspace() {
  const vault = useVault()
  const { notes, notebooks, notebookTitle, tagTitle, source, activeNote, listLoading, hasMore } = vault
  const [selectedId, setSelectedId] = React.useState<string | null>(null)
  const [query, setQuery] = React.useState('')
  const [notebookFilter, setNotebookFilter] = React.useState<string | null>(null)
  const [tagFilter, setTagFilter] = React.useState<string | null>(null)
  const [vaultState, setVaultState] = React.useState<VaultState>('unlocked')
  const { state: previewState } = Route.useSearch()
  const [syncState, setSyncState] = React.useState<SyncState>(
    isSyncState(previewState) ? previewState : 'synced',
  )
  const [lastSync, setLastSync] = React.useState<number>(0)
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

  // The backend owns filtering and search. Debounce typing so a keystroke
  // does not turn into a request per character; filters apply immediately.
  React.useEffect(() => {
    const delay = query.trim() ? 250 : 0
    const t = window.setTimeout(() => {
      void vault.loadList({
        query: query.trim(),
        notebookId: notebookFilter,
        tagId: tagFilter,
      })
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
    void vault.selectNote(selectedId)
  }, [selectedId, vault.selectNote])

  // Set after mount so SSR and first client render agree (avoids a hydration mismatch).
  React.useEffect(() => {
    setLastSync(Date.now() - 4 * 60_000)
  }, [])

  const runSync = () => {
    setSyncState('syncing')
    window.setTimeout(() => {
      setLastSync(Date.now())
      if (source === 'live') {
        vault.reload()
        setSyncState('synced')
      } else {
        setSyncState(notes.some((n) => n.isConflict) ? 'conflict' : 'synced')
      }
    }, source === 'live' ? 400 : 2400)
  }

  const q = query.trim()
  const selected = activeNote

  const selectNote = (id: string) => {
    setSelectedId(id)
    setMobileView('edit')
  }

  const listTitle = q
    ? `Search “${q}”`
    : tagFilter
      ? `Tag: ${tagTitle(tagFilter)}`
      : notebookFilter
        ? notebookTitle(notebookFilter)
        : 'All notes'

  const createNote = async () => {
    const id = await vault.createNote({ notebookId: notebookFilter ?? '' })
    setSelectedId(id)
    setMobileView('edit')
  }

  const toggleTodo = (id: string) => {
    void vault.toggleTodo(id)
  }

  const changeBody = (id: string, body: string) => {
    void vault.patchNote(id, { body })
  }

  return (
    <div className="flex h-dvh flex-col">
      <TopBar
        query={query}
        onQuery={setQuery}
        syncState={syncState}
        onSync={runSync}
        vault={vaultState}
        onToggleVault={() => setVaultState((v) => (v === 'unlocked' ? 'locked' : 'unlocked'))}
        onNewNote={() => void createNote()}
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
            notes={notes}
            selectedId={selectedId}
            onSelect={selectNote}
            query={q}
            onClearQuery={() => setQuery('')}
            listTitle={listTitle}
            onOpenNav={() => setNavOpen(true)}
            hasMore={hasMore}
            loading={listLoading}
            onLoadMore={() => void vault.loadMore()}
          />
        </div>

        <div className={`min-h-0 min-w-0 ${mobileView === 'list' ? 'hidden lg:block' : 'block'}`}>
          <EditorPane
            note={selected}
            loading={vault.activeLoading}
            vault={vaultState}
            onUnlock={() => setVaultState('unlocked')}
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
        hasMore={hasMore}
        source={source}
        error={vault.error}
      />
    </div>
  )
}
