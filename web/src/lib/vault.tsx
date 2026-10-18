// Summary lists stay paginated; bodies are fetched only for selected notes.
import * as React from 'react'
import type { Note, Notebook, Tag } from './mock-data'
import * as mock from './mock-data'
import * as api from './api'
export type { Note, Notebook, Tag } from './mock-data'

export type VaultSource = 'mock' | 'loading' | 'live' | 'error'
export type SaveState = 'saved' | 'saving' | 'error'
export type ListFilter = { query?: string; notebookId?: string | null; tagId?: string | null }
export type Vault = {
  notebooks: Notebook[]; tags: Tag[]; notes: Note[]
  source: VaultSource; error: string | null; canWrite: boolean; saveState: SaveState
  hasMore: boolean; listLoading: boolean; countsExact: boolean
  activeNote: Note | null; activeLoading: boolean
  notebookTitle: (id: string) => string
  tagTitle: (id: string) => string
  noteCountByNotebook: (id: string) => number
  loadList: (filter: ListFilter) => Promise<void>
  loadMore: () => Promise<void>
  selectNote: (id: string) => Promise<void>
  createNote: (init?: Partial<Note>) => Promise<string>
  patchNote: (id: string, patch: Partial<Note>) => Promise<void>
  toggleTodo: (id: string) => Promise<void>
  deleteNote: (id: string) => Promise<void>
  createNotebook: (title: string, parentId?: string | null) => Promise<string>
  createTag: (title: string) => Promise<string>
  setNoteTags: (id: string, tagIds: string[]) => Promise<void>
  reload: () => Promise<void>
}
type Edit = Pick<Partial<Note>, 'title' | 'body' | 'notebookId' | 'isTodo' | 'todoCompleted' | 'todoDue'>
const Ctx = React.createContext<Vault | null>(null)
const newID = () =>
  (typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID().replace(/-/g, '')
    : Math.random().toString(16).slice(2) + Math.random().toString(16).slice(2)
  ).slice(0, 32)

function descendants(id: string, notebooks: Notebook[]): Set<string> {
  const ids = new Set([id])
  for (const parent of ids) {
    for (const nb of notebooks) if (nb.parentId === parent) ids.add(nb.id)
  }
  return ids
}

function applyEdit(note: Note, edit?: Partial<Note>): Note {
  return edit ? { ...note, ...edit, ...(edit.body !== undefined ? { excerpt: api.noteExcerpt(edit.body) } : {}) } : note
}

export function VaultProvider({ children }: { children: React.ReactNode }) {
  const live = api.isLive()
  const [notebooks, setNotebooks] = React.useState<Notebook[]>(live ? [] : mock.notebooks)
  const [tags, setTags] = React.useState<Tag[]>(live ? [] : mock.tags)
  const [notes, setNotes] = React.useState<Note[]>(live ? [] : mock.notes)
  const [source, setSource] = React.useState<VaultSource>(live ? 'loading' : 'mock')
  const [error, setError] = React.useState<string | null>(null)
  const [saveState, setSaveState] = React.useState<SaveState>('saved')
  const [metaReady, setMetaReady] = React.useState(!live)
  const [hasMore, setHasMore] = React.useState(false)
  const [listLoading, setListLoading] = React.useState(false)
  const [countsExact, setCountsExact] = React.useState(!live)
  const [activeNote, setActiveNote] = React.useState<Note | null>(null)
  const [activeLoading, setActiveLoading] = React.useState(false)
  const active = React.useRef<Note | null>(null)
  const rows = React.useRef(notes)
  rows.current = notes
  const demo = React.useRef(mock.notes)
  const pending = React.useRef(new Map<string, Edit>())
  const edits = React.useRef(new Map<string, Partial<Note>>())
  const recoveryError = React.useRef<unknown>(null)
  const timers = React.useRef(new Map<string, number>())
  const flights = React.useRef(new Map<string, Promise<void>>())
  const seq = React.useRef(0)
  const loadSeq = React.useRef(0)
  const nextCursor = React.useRef('')
  const lastFilter = React.useRef<ListFilter>({})
  const canWrite = !live || (source === 'live' && metaReady && !listLoading)

  const fail = React.useCallback((e: unknown) => {
    setError(e instanceof Error ? e.message : String(e))
    if (api.isLive()) setSource('error')
  }, [])
  const showActive = React.useCallback((note: Note | null) => {
    active.current = note
    setActiveNote(note)
  }, [])
  const persist = React.useCallback(() => {
    // Synchronous before acknowledging an edit: unload handlers cannot reliably send HTTP.
    localStorage.setItem(api.recoveryKey, JSON.stringify([...pending.current]))
  }, [])
  const overlay = React.useCallback((note: Note) => applyEdit(note, pending.current.get(note.id)), [])

  const flushSave = React.useCallback((id: string): Promise<void> => {
    window.clearTimeout(timers.current.get(id))
    timers.current.delete(id)
    const existing = flights.current.get(id)
    if (existing) return existing
    if (!pending.current.has(id)) return Promise.resolve()
    const task = (async () => {
      try {
        while (pending.current.has(id)) {
          const edit = pending.current.get(id)!
          const body: Record<string, unknown> = {}
          if (edit.body !== undefined) body.body = edit.body
          if (edit.title !== undefined) body.title = edit.title
          if (edit.notebookId !== undefined) body.parent_id = edit.notebookId
          if (edit.isTodo !== undefined) body.is_todo = edit.isTodo ? 1 : 0
          if (edit.todoCompleted !== undefined) body.todo_completed = edit.todoCompleted ? 1 : 0
          if (edit.todoDue !== undefined) body.todo_due = edit.todoDue ?? 0
          setSaveState((prev) => prev === 'error' ? prev : 'saving')
          const saved = await api.updateNoteApi(id, body)
          if (pending.current.get(id) === edit) {
            pending.current.delete(id)
            try { persist() } catch (e) { pending.current.set(id, edit); throw e }
          }
          // Preserve associations absent from PUT, and never replace newer local fields.
          setNotes((prev) => prev.map((n) => n.id === id
            ? { ...overlay({ ...saved, tagIds: n.tagIds }), body: '', } : n))
          if (active.current?.id === id) {
            showActive(overlay({ ...saved, tagIds: active.current.tagIds }))
          }
        }
        if (!pending.current.size) setSaveState('saved')
      } catch (e) {
        setSaveState('error')
        fail(e)
        throw e
      }
    })().finally(() => { flights.current.delete(id) })
    flights.current.set(id, task)
    return task
  }, [fail, overlay, persist, showActive])

  const loadMeta = React.useCallback(async () => {
    if (!api.isLive()) return
    const [nbs, tgs] = await Promise.all([api.fetchNotebooks(), api.fetchTags()])
    setNotebooks(nbs)
    setTags(tgs)
    setMetaReady(true)
  }, [])

  const loadList = React.useCallback(async (filter: ListFilter) => {
    lastFilter.current = filter
    const my = ++seq.current
    const before = new Map(edits.current)
    const recovering = new Map(pending.current)
    setListLoading(true)
    try {
      let page: { notes: Note[]; hasMore: boolean; nextCursor: string }
      if (api.isLive()) page = await api.fetchNoteSummaries(filter)
      else {
        const scope = filter.notebookId ? descendants(filter.notebookId, notebooks) : null
        const q = filter.query?.trim().toLowerCase()
        page = { notes: demo.current.filter((n) => (!scope || scope.has(n.notebookId))
          && (!filter.tagId || n.tagIds.includes(filter.tagId))
          && (!q || [n.title, n.body, notebooks.find((b) => b.id === n.notebookId)?.title,
            ...n.tagIds.map((id) => tags.find((t) => t.id === id)?.title)].join(' ').toLowerCase().includes(q))),
          hasMore: false, nextCursor: '' }
      }
      if (my !== seq.current) return
      setNotes(page.notes.map((n) => overlay(applyEdit(n, recovering.has(n.id) || before.get(n.id) !== edits.current.get(n.id) ? edits.current.get(n.id) : undefined))))
      if (active.current && !page.notes.some((n) => n.id === active.current!.id) && !pending.current.has(active.current.id)) {
        ++loadSeq.current
        showActive(null)
        setActiveLoading(false)
      }
      nextCursor.current = page.nextCursor
      setHasMore(page.hasMore)
      setCountsExact(!page.hasMore && !filter.query && !filter.notebookId && !filter.tagId)
      // An independent metadata or save failure must not be hidden by a successful list.
      setSource((prev) => prev === 'error' ? prev : api.isLive() ? 'live' : 'mock')
    } catch (e) { if (my === seq.current) fail(e) }
    finally { if (my === seq.current) setListLoading(false) }
  }, [fail, notebooks, tags, overlay, showActive])

  const loadMore = React.useCallback(async () => {
    if (!api.isLive() || !nextCursor.current || listLoading) return
    const my = seq.current
    setListLoading(true)
    try {
      const page = await api.fetchNoteSummaries({ ...lastFilter.current, cursor: nextCursor.current })
      if (my !== seq.current) return
      setNotes((prev) => [...prev, ...page.notes.filter((n) => !prev.some((p) => p.id === n.id)).map(overlay)])
      nextCursor.current = page.nextCursor
      setHasMore(page.hasMore)
      if (!page.hasMore && !lastFilter.current.query && !lastFilter.current.notebookId && !lastFilter.current.tagId) setCountsExact(true)
    } catch (e) { if (my === seq.current) fail(e) }
    finally { if (my === seq.current) setListLoading(false) }
  }, [fail, listLoading, overlay])

  const selectNote = React.useCallback(async (id: string) => {
    if (active.current?.id === id) return
    if (active.current) void flushSave(active.current.id).catch(() => {})
    const my = ++loadSeq.current
    const before = edits.current.get(id)
    const recovering = pending.current.has(id)
    const summary = rows.current.find((n) => n.id === id) ?? null
    showActive(summary ? overlay(summary) : null)
    if (!api.isLive()) return
    setActiveLoading(true)
    try {
      const full = await api.fetchNote(id)
      if (my !== loadSeq.current) return
      full.tagIds = active.current?.id === id ? active.current.tagIds : summary?.tagIds ?? []
      showActive(overlay(applyEdit(full, recovering || before !== edits.current.get(id) ? edits.current.get(id) : undefined)))
    } catch (e) { if (my === loadSeq.current) fail(e) }
    finally { if (my === loadSeq.current) setActiveLoading(false) }
  }, [fail, flushSave, overlay, showActive])

  React.useEffect(() => {
    if (!api.isLive()) return
    try {
      const stored = localStorage.getItem(api.recoveryKey)
      if (stored) {
        const entries: unknown = JSON.parse(stored)
        const fields: Record<string, string> = { title: 'string', body: 'string', notebookId: 'string', isTodo: 'boolean', todoCompleted: 'boolean', todoDue: 'number' }
        if (!Array.isArray(entries) || entries.some((entry) => !Array.isArray(entry)
          || typeof entry[0] !== 'string' || !entry[1] || typeof entry[1] !== 'object'
          || Object.entries(entry[1]).some(([key, value]) => !(key in fields)
            || (typeof value !== fields[key] && !(key === 'todoDue' && value === null))))) {
          throw new Error('Pending-edit recovery data is invalid; preserve local storage before resetting it.')
        }
        pending.current = new Map(entries as [string, Edit][])
        edits.current = new Map(pending.current)
        if (pending.current.size) setSaveState('saving')
      }
      void Promise.all([...pending.current.keys()].map(flushSave)).catch(() => {})
    } catch (e) { recoveryError.current = e; setSaveState('error'); fail(e) }
    void loadMeta().catch(fail)
    const online = () => { for (const id of pending.current.keys()) void flushSave(id).catch(() => {}) }
    window.addEventListener('online', online)
    return () => {
      window.removeEventListener('online', online)
      for (const timer of timers.current.values()) window.clearTimeout(timer)
    }
  }, [fail, flushSave, loadMeta])

  function requireWrite() {
    if (!canWrite) throw new Error('Local API unavailable or loading. Retry before making changes.')
  }
  async function mutate<T>(action: () => Promise<T>): Promise<T> {
    requireWrite()
    try { return await action() } catch (e) { fail(e); throw e }
  }
  async function patchNote(id: string, patch: Partial<Note>) {
    requireWrite()
    const edit: Edit = {}
    for (const key of ['body', 'title', 'notebookId', 'isTodo', 'todoCompleted', 'todoDue'] as const) {
      if (patch[key] !== undefined) Object.assign(edit, { [key]: patch[key] })
    }
    if (!Object.keys(edit).length) return
    if (live) {
      const prior = pending.current.get(id)
      pending.current.set(id, { ...prior, ...edit })
      try { persist() } catch (e) {
        if (prior) pending.current.set(id, prior); else pending.current.delete(id)
        setSaveState('error'); fail(e); throw e
      }
      setSaveState((prev) => prev === 'error' ? prev : 'saving')
    }
    edits.current.set(id, { ...edits.current.get(id), ...edit })
    const update = (n: Note) => n.id === id ? { ...applyEdit(n, edit), updatedTime: Date.now() } : n
    setNotes((prev) => prev.map(update))
    if (active.current) showActive(update(active.current))
    if (!live) { demo.current = demo.current.map(update); return }
    window.clearTimeout(timers.current.get(id))
    if (Object.keys(edit).length === 1 && edit.body !== undefined) {
      timers.current.set(id, window.setTimeout(() => { void flushSave(id).catch(() => {}) }, 600))
    } else await flushSave(id)
  }
  async function createNote(init?: Partial<Note>): Promise<string> {
    return mutate(async () => {
      const fresh: Note = { id: newID(), title: init?.title ?? 'Untitled note', body: init?.body ?? '# Untitled note\n\n',
        notebookId: init?.notebookId ?? '', tagIds: [], createdTime: Date.now(), updatedTime: Date.now(),
        isTodo: false, todoCompleted: false, todoDue: null, isConflict: false, conflictOriginalTitle: null }
      const saved = live ? await api.createNoteApi(fresh) : fresh
      saved.excerpt = api.noteExcerpt(saved.body)
      ++loadSeq.current
      ++seq.current
      setListLoading(false)
      setActiveLoading(false)
      setNotes((prev) => [saved, ...prev])
      if (!live) demo.current = [saved, ...demo.current]
      showActive(saved)
      return saved.id
    })
  }
  async function deleteNote(id: string) {
    await mutate(async () => {
      if (live) { await flushSave(id); await api.deleteNoteApi(id) }
      ++seq.current
      setListLoading(false)
      setNotes((prev) => prev.filter((n) => n.id !== id))
      if (!live) demo.current = demo.current.filter((n) => n.id !== id)
      if (active.current?.id === id) { ++loadSeq.current; showActive(null); setActiveLoading(false) }
      edits.current.delete(id)
    })
  }
  async function createNotebook(title: string, parentId: string | null = null) {
    return mutate(async () => {
      const nb = live ? await api.createNotebookApi(title, parentId) : { id: newID(), title, parentId }
      setNotebooks((prev) => [...prev, nb])
      return nb.id
    })
  }
  async function createTag(title: string) {
    return mutate(async () => {
      const tag = live ? await api.createTagApi(title) : { id: newID(), title }
      setTags((prev) => [...prev, tag])
      return tag.id
    })
  }
  async function setNoteTags(id: string, tagIds: string[]) {
    await mutate(async () => {
      try {
        if (live) await api.setNoteTagsApi(id, tagIds)
      } catch (e) {
        // Multiple API routes are not transactional: show actual partial results on failure.
        try {
          const actual = await api.fetchNoteTags(id)
          updateTags(actual.map((t) => t.id))
        } catch { /* Keep existing associations until the connection can be retried. */ }
        throw e
      }
      updateTags([...new Set(tagIds)])
    })
    function updateTags(ids: string[]) {
      edits.current.set(id, { ...edits.current.get(id), tagIds: ids })
      const update = (n: Note) => n.id === id ? { ...n, tagIds: ids } : n
      setNotes((prev) => prev.map(update))
      if (active.current) showActive(update(active.current))
      if (!live) demo.current = demo.current.map(update)
    }
  }
  const reload = React.useCallback(async () => {
    setSource(api.isLive() ? 'loading' : 'mock')
    setError(null)
    try {
      if (recoveryError.current) throw recoveryError.current
      await Promise.all([...pending.current.keys()].map(flushSave))
      await loadMeta()
      await loadList(lastFilter.current)
      const id = active.current?.id
      if (id && api.isLive()) {
        const my = ++loadSeq.current
        const before = edits.current.get(id)
        setActiveLoading(true)
        try {
          const full = await api.fetchNote(id)
          if (my === loadSeq.current) showActive(overlay(applyEdit({ ...full, tagIds: active.current?.tagIds ?? [] },
            before !== edits.current.get(id) ? edits.current.get(id) : undefined)))
        } finally { if (my === loadSeq.current) setActiveLoading(false) }
      }
    } catch (e) { fail(e) }
  }, [fail, flushSave, loadList, loadMeta, overlay, showActive])

  const value: Vault = {
    notebooks, tags, notes, source, error, canWrite, saveState, hasMore, listLoading, countsExact, activeNote, activeLoading,
    notebookTitle: (id) => notebooks.find((n) => n.id === id)?.title ?? '—',
    tagTitle: (id) => tags.find((t) => t.id === id)?.title ?? id,
    noteCountByNotebook: (id) => { const ids = descendants(id, notebooks); return notes.filter((n) => ids.has(n.notebookId)).length },
    loadList, loadMore, selectNote, createNote, patchNote, deleteNote, createNotebook, createTag, setNoteTags, reload,
    toggleTodo: async (id) => { const n = rows.current.find((n) => n.id === id) ?? active.current; await patchNote(id, { todoCompleted: !n?.todoCompleted }) },
  }
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useVault(): Vault {
  const v = React.useContext(Ctx)
  if (!v) throw new Error('useVault must be used inside <VaultProvider>')
  return v
}
