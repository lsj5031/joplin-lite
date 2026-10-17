// Vault data source: the components consume notebooks/notes/tags through this
// provider, so the shell stays agnostic of where the data comes from — the
// synthetic prototype data (no backend configured) or the live gojoplin Data
// API (VITE_GOJOPLIN_URL set).
//
// Live lists are summary rows: the provider holds only what the list renders,
// pages them with the backend's keyset cursor, and asks the backend to filter
// and search. A note body is fetched when the note is selected. Mutations are
// optimistic locally and written through to the API when live.
import * as React from 'react'
import type { Note, Notebook, Tag } from './mock-data'
import * as mock from './mock-data'
import * as api from './api'

export type VaultSource = 'mock' | 'loading' | 'live'

export type ListFilter = {
  query?: string
  notebookId?: string | null
  tagId?: string | null
}

type Vault = {
  notebooks: Notebook[]
  tags: Tag[]
  notes: Note[]
  source: VaultSource
  error: string | null
  hasMore: boolean
  listLoading: boolean
  /** countsExact is true only when the loaded rows are the whole library, so
   * the sidebar must not present page-limited counts as totals. */
  countsExact: boolean
  activeNote: Note | null
  activeLoading: boolean
  notebookTitle: (id: string) => string
  tagTitle: (id: string) => string
  noteCountByNotebook: (id: string) => number
  /** loadList replaces the list with the first page for a filter. */
  loadList: (filter: ListFilter) => Promise<void>
  /** loadMore appends the next keyset page. */
  loadMore: () => Promise<void>
  /** selectNote loads the full note (body included) for the editor. */
  selectNote: (id: string) => Promise<void>
  /** createNote returns the new note's id so the caller can select it. */
  createNote: (init?: Partial<Note>) => Promise<string>
  patchNote: (id: string, patch: Partial<Note>) => Promise<void>
  toggleTodo: (id: string) => Promise<void>
  reload: () => void
}

const Ctx = React.createContext<Vault | null>(null)

const newID = () =>
  (typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID().replace(/-/g, '')
    : Math.random().toString(16).slice(2) + Math.random().toString(16).slice(2)
  ).slice(0, 32)

const EMPTY_FILTER: ListFilter = { query: '', notebookId: null, tagId: null }

/** mockPage keeps the synthetic prototype usable without a backend. */
function mockPage(f: ListFilter): { notes: Note[]; hasMore: boolean; nextCursor: string } {
  const q = (f.query ?? '').trim().toLowerCase()
  let rows = mock.notes
  const notebookId = f.notebookId
  if (notebookId) {
    const scope = [notebookId, ...mock.notebooks.filter((b) => b.parentId === notebookId).map((b) => b.id)]
    rows = rows.filter((n) => scope.includes(n.notebookId))
  }
  const tagId = f.tagId
  if (tagId) rows = rows.filter((n) => n.tagIds.includes(tagId))
  if (q) {
    rows = rows.filter((n) =>
      [n.title, n.body, mock.notebookTitle(n.notebookId), ...n.tagIds.map(mock.tagTitle)]
        .join(' ')
        .toLowerCase()
        .includes(q),
    )
  }
  return { notes: rows, hasMore: false, nextCursor: '' }
}

export function VaultProvider({ children }: { children: React.ReactNode }) {
  const [notebooks, setNotebooks] = React.useState<Notebook[]>(mock.notebooks)
  const [tags, setTags] = React.useState<Tag[]>(mock.tags)
  const [notes, setNotes] = React.useState<Note[]>(api.isLive() ? [] : mock.notes)
  const [source, setSource] = React.useState<VaultSource>(api.isLive() ? 'loading' : 'mock')
  const [error, setError] = React.useState<string | null>(null)
  const [hasMore, setHasMore] = React.useState(false)
  const [listLoading, setListLoading] = React.useState(false)
  const [countsExact, setCountsExact] = React.useState(!api.isLive())
  const [activeNote, setActiveNote] = React.useState<Note | null>(null)
  const [activeLoading, setActiveLoading] = React.useState(false)

  const seq = React.useRef(0)
  const nextCursor = React.useRef('')
  const lastFilter = React.useRef<ListFilter>(EMPTY_FILTER)
  const activeRef = React.useRef<Note | null>(null)
  React.useEffect(() => {
    activeRef.current = activeNote
  }, [activeNote])

  const loadMeta = React.useCallback(async () => {
    if (!api.isLive()) {
      setNotebooks(mock.notebooks)
      setTags(mock.tags)
      setSource('mock')
      return
    }
    const [nbs, tgs] = await Promise.all([api.fetchNotebooks(), api.fetchTags()])
    setNotebooks(nbs)
    setTags(tgs)
  }, [])

  const loadList = React.useCallback(async (filter: ListFilter) => {
    lastFilter.current = filter
    const my = ++seq.current
    setListLoading(true)
    setError(null)
    try {
      if (!api.isLive()) {
        const page = mockPage(filter)
        if (my !== seq.current) return
        setNotes(page.notes)
        if (activeRef.current && !page.notes.some((n) => n.id === activeRef.current!.id)) {
          setActiveNote(null)
        }
        setHasMore(page.hasMore)
        setCountsExact(true)
        setSource('mock')
        return
      }
      const page = await api.fetchNoteSummaries(filter)
      if (my !== seq.current) return
      setNotes(page.notes)
      if (activeRef.current && !page.notes.some((n) => n.id === activeRef.current!.id)) {
        setActiveNote(null)
      }
      setHasMore(page.hasMore)
      nextCursor.current = page.nextCursor
      setCountsExact(!page.hasMore && !filter.query && !filter.notebookId && !filter.tagId)
      setSource('live')
    } catch (e) {
      if (my !== seq.current) return
      setError(e instanceof Error ? e.message : String(e))
      // Keep the shell usable: fall back to the synthetic library.
      const page = mockPage(filter)
      setNotes(page.notes)
      setHasMore(false)
      setCountsExact(true)
      setSource('mock')
    } finally {
      if (my === seq.current) setListLoading(false)
    }
  }, [])

  const loadMore = React.useCallback(async () => {
    if (!api.isLive() || !nextCursor.current) return
    const my = seq.current
    setListLoading(true)
    try {
      const page = await api.fetchNoteSummaries({ ...lastFilter.current, cursor: nextCursor.current })
      if (my !== seq.current) return
      setNotes((prev) => [...prev, ...page.notes])
      setHasMore(page.hasMore)
      nextCursor.current = page.nextCursor
      setCountsExact((prev) => prev && !page.hasMore)
    } catch (e) {
      if (my !== seq.current) return
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      if (my === seq.current) setListLoading(false)
    }
  }, [])

  // Body writes are debounced: a keystroke must not become one PUT of the
  // whole note body. The optimistic state updates immediately; the network
  // write is coalesced and flushed when the note changes or the editor closes.
  const pendingSave = React.useRef(new Map<string, { timer: number; patch: Record<string, unknown> }>())
  const sendPatch = React.useCallback(async (id: string, body: Record<string, unknown>) => {
    try {
      const saved = await api.updateNoteApi(id, body)
      // Do not let a slow response clobber newer local keystrokes.
      setNotes((prev) =>
        prev.map((n) => {
          if (n.id !== id) return n
          const keepBody = n.body !== saved.body
          return { ...n, ...saved, body: keepBody ? n.body : '', excerpt: n.excerpt }
        }),
      )
      setActiveNote((prev) => {
        if (!prev || prev.id !== id) return prev
        if (prev.body !== saved.body) return { ...prev, updatedTime: saved.updatedTime }
        return { ...saved, tagIds: prev.tagIds }
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [])

  const flushSave = React.useCallback(
    (id: string) => {
      const pending = pendingSave.current.get(id)
      if (!pending) return
      window.clearTimeout(pending.timer)
      pendingSave.current.delete(id)
      void sendPatch(id, pending.patch)
    },
    [sendPatch],
  )

  const flushAllSaves = React.useCallback(() => {
    for (const [id, pending] of pendingSave.current) {
      window.clearTimeout(pending.timer)
      pendingSave.current.delete(id)
      void sendPatch(id, pending.patch)
    }
  }, [sendPatch])

  // Never lose the last keystrokes when the editor goes away.
  React.useEffect(() => () => flushAllSaves(), [flushAllSaves])

  const loadSeq = React.useRef(0)

  const selectNote = React.useCallback(async (id: string) => {
    if (activeRef.current?.id === id) return
    if (activeRef.current) flushSave(activeRef.current.id)
    const summary = notes.find((n) => n.id === id) ?? null
    const my = ++loadSeq.current
    setActiveLoading(true)
    setActiveNote(summary)

    if (!api.isLive()) {
      setActiveLoading(false)
      return
    }
    try {
      const full = await api.fetchNote(id)
      // A slower response for a note the user already navigated away from
      // must not replace the current selection.
      if (my !== loadSeq.current) return
      // The list row already knows this note's tags; the full note endpoint
      // does not return them, so carry them over instead of a second call.
      full.tagIds = summary?.tagIds ?? []
      setActiveNote(full)
    } catch (e) {
      if (my !== loadSeq.current) return
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      if (my === loadSeq.current) setActiveLoading(false)
    }
  }, [notes, flushSave])

  React.useEffect(() => {
    void loadMeta().catch((e) => {
      setError(e instanceof Error ? e.message : String(e))
      setSource('mock')
    })
  }, [loadMeta])

  const notebookTitle = React.useCallback(
    (id: string) => notebooks.find((n) => n.id === id)?.title ?? '—',
    [notebooks],
  )
  const tagTitle = React.useCallback((id: string) => tags.find((t) => t.id === id)?.title ?? id, [tags])
  const noteCountByNotebook = React.useCallback(
    (id: string) => {
      const children = notebooks.filter((n) => n.parentId === id).map((n) => n.id)
      const ids = [id, ...children]
      return notes.filter((n) => ids.includes(n.notebookId)).length
    },
    [notebooks, notes],
  )

  const createNote = React.useCallback(
    async (init?: Partial<Note>): Promise<string> => {
      const now = Date.now()
      const fresh: Note = {
        id: newID(),
        title: init?.title ?? 'Untitled note',
        body: init?.body ?? '# Untitled note\n\n',
        notebookId: init?.notebookId ?? '',
        tagIds: [],
        createdTime: now,
        updatedTime: now,
        isTodo: false,
        todoCompleted: false,
        todoDue: null,
        isConflict: false,
        conflictOriginalTitle: null,
        excerpt: '',
      }
      setNotes((prev) => [fresh, ...prev])
      setActiveNote(fresh)
      if (source === 'live') {
        try {
          const saved = await api.createNoteApi({
            title: fresh.title,
            body: fresh.body,
            notebookId: fresh.notebookId,
          })
          setNotes((prev) => prev.map((n) => (n.id === fresh.id ? { ...saved, excerpt: fresh.excerpt } : n)))
          setActiveNote(saved)
          return saved.id
        } catch (e) {
          setError(e instanceof Error ? e.message : String(e))
        }
      }
      return fresh.id
    },
    [source],
  )

  const patchNote = React.useCallback(
    async (id: string, patch: Partial<Note>) => {
      const now = Date.now()
      setNotes((prev) => prev.map((n) => (n.id === id ? { ...n, ...patch, updatedTime: now } : n)))
      setActiveNote((prev) => (prev && prev.id === id ? { ...prev, ...patch, updatedTime: now } : prev))
      if (source !== 'live') return

      const body: Record<string, unknown> = {}
      if (patch.title !== undefined) body.title = patch.title
      if (patch.body !== undefined) body.body = patch.body
      if (patch.isTodo !== undefined) body.is_todo = patch.isTodo ? 1 : 0
      if (patch.todoCompleted !== undefined) body.todo_completed = patch.todoCompleted ? 1 : 0

      // Body-only edits are the typing path: coalesce them. Any other edit
      // flushes a pending body write first so the server sees them in order.
      const bodyOnly = patch.body !== undefined && Object.keys(body).length === 1
      if (bodyOnly) {
        const pending = pendingSave.current.get(id)
        if (pending) window.clearTimeout(pending.timer)
        const timer = window.setTimeout(() => {
          pendingSave.current.delete(id)
          void sendPatch(id, body)
        }, 600)
        pendingSave.current.set(id, { timer, patch: body })
        return
      }
      flushSave(id)
      await sendPatch(id, body)
    },
    [source, sendPatch, flushSave],
  )

  const toggleTodo = React.useCallback(
    async (id: string) => {
      const n = notes.find((x) => x.id === id) ?? activeRef.current
      await patchNote(id, { todoCompleted: !n?.todoCompleted })
    },
    [notes, patchNote],
  )

  const reload = React.useCallback(() => {
    flushAllSaves()
    void loadMeta().catch((e) => setError(e instanceof Error ? e.message : String(e)))
    void loadList(lastFilter.current)
  }, [loadMeta, loadList, flushAllSaves])

  const value: Vault = {
    notebooks,
    tags,
    notes,
    source,
    error,
    hasMore,
    listLoading,
    countsExact,
    activeNote,
    activeLoading,
    notebookTitle,
    tagTitle,
    noteCountByNotebook,
    loadList,
    loadMore,
    selectNote,
    createNote,
    patchNote,
    toggleTodo,
    reload,
  }

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useVault(): Vault {
  const v = React.useContext(Ctx)
  if (!v) throw new Error('useVault must be used inside <VaultProvider>')
  return v
}
