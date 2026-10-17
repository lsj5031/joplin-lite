// Thin HTTP client over the local gojoplin Clipper/Data API.
//
// This is the "api" half of the INTENT seam: components talk to the vault
// provider, the provider talks to this module, and this module talks to
// whatever backend is configured. When VITE_GOJOPLIN_URL is unset the UI falls
// back to the synthetic prototype data (see vault.tsx), so the docs/screenshots
// keep working without a server.
//
// Lists use the additive `summary=1` projection: rows carry an excerpt and no
// body, and are paged by keyset cursor. The plain Data API endpoints still
// return full notes for Joplin clients; the body for the open note is fetched
// with GET /notes/{id}.
//
// Configure a live backend with:
//   VITE_GOJOPLIN_URL=http://127.0.0.1:41888
//   VITE_GOJOPLIN_TOKEN=<api token>
//
// The desktop shell (shell/) starts the API on an OS-assigned port with a
// per-session token and injects both before this module loads, so the same
// build runs in a browser (env) and in the app (runtime config) unchanged.
import type { Note, Notebook, Tag } from './mock-data'

const env = (import.meta as unknown as { env?: Record<string, string | undefined> }).env ?? {}
const runtime = (
  globalThis as { __GOJOPLIN__?: { base?: string; token?: string; profile?: string } }
).__GOJOPLIN__
const BASE: string = (runtime?.base ?? env.VITE_GOJOPLIN_URL ?? '').replace(/\/+$/, '')
const TOKEN: string = runtime?.token ?? env.VITE_GOJOPLIN_TOKEN ?? ''

/** isLive reports whether a backend URL is configured. */
export const isLive = (): boolean => BASE !== ''

// Native ports change between launches; recovery follows the database profile.
export const recoveryKey = `joplin-lite:pending:${runtime?.profile ?? BASE}`

/** LIST_PAGE is the list page size. Keyset paging means this is a ceiling on
 * rows shipped, not an offset the server must scan past. */
export const LIST_PAGE = 50

function withToken(path: string): string {
  const sep = path.includes('?') ? '&' : '?'
  return `${BASE}${path}${sep}token=${encodeURIComponent(TOKEN)}`
}

async function apiError(res: Response, request: string): Promise<never> {
  const detail = await res.json().catch(() => null) as { error?: unknown } | null
  throw new Error(`${request} → ${res.status}${typeof detail?.error === 'string' ? `: ${detail.error}` : ''}`)
}

async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(withToken(path))
  if (!res.ok) return apiError(res, `GET ${path}`)
  return (await res.json()) as T
}

async function apiSend<T>(method: string, path: string, body: unknown): Promise<T> {
  const res = await fetch(withToken(path), {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) return apiError(res, `${method} ${path}`)
  return (await res.json()) as T
}

type FolderDTO = { id: string; title: string; parent_id?: string }
type TagDTO = { id: string; title: string }
type NoteDTO = {
  id: string
  parent_id?: string
  title: string
  body?: string
  created_time: number
  updated_time: number
  is_todo?: number
  todo_completed?: number
  todo_due?: number
  is_conflict?: number
  conflict_original_id?: string
}

type SummaryDTO = Omit<NoteDTO, 'body'> & {
  excerpt?: string
  tag_ids?: string[]
}

type CursorPage<T> = { items?: T[]; has_more?: boolean; next_cursor?: string }

// The Data API paginates at limit<=100 with {items, has_more}.
async function fetchAll<T>(path: string): Promise<T[]> {
  const out: T[] = []
  for (let page = 1; ; page++) {
    const sep = path.includes('?') ? '&' : '?'
    const page_ = await apiGet<CursorPage<T>>(`${path}${sep}limit=100&page=${page}`)
    out.push(...(page_.items ?? []))
    if (!page_.has_more) break
  }
  return out
}

export async function fetchNotebooks(): Promise<Notebook[]> {
  const items = await fetchAll<FolderDTO>('/folders')
  return items.map((f) => ({ id: f.id, title: f.title, parentId: f.parent_id || null }))
}

export async function fetchTags(): Promise<Tag[]> {
  const items = await fetchAll<TagDTO>('/tags')
  return items.map((t) => ({ id: t.id, title: t.title }))
}

const toNote = (d: NoteDTO): Note => ({
  id: d.id,
  title: d.title,
  body: d.body ?? '',
  notebookId: d.parent_id ?? '',
  tagIds: [],
  createdTime: d.created_time,
  updatedTime: d.updated_time,
  isTodo: !!d.is_todo,
  todoCompleted: !!d.todo_completed,
  todoDue: d.todo_due ? d.todo_due : null,
  isConflict: !!d.is_conflict,
  conflictOriginalTitle: d.conflict_original_id ? 'conflicted copy' : null,
  excerpt: noteExcerpt(d.body ?? ''),
})

/** Match the summary API's first-line, 140-rune excerpt. */
export function noteExcerpt(body: string): string {
  const line = body.trim().split(/[\r\n]/, 1)[0].replace(/^[#> \t]+/, '').replace(/\s+/g, ' ')
  const chars = Array.from(line)
  return chars.length > 140 ? chars.slice(0, 140).join('') + '…' : line
}

const toSummary = (d: SummaryDTO): Note => ({
  ...toNote({ ...d, body: '' }),
  excerpt: d.excerpt ?? '',
  tagIds: d.tag_ids ?? [],
})

export type ListFilter = {
  query?: string
  notebookId?: string | null
  tagId?: string | null
  cursor?: string
}

/**
 * fetchNoteSummaries asks the backend for the next page of list rows.
 * Search goes to FTS5 (`/search`), and notebook/tag scopes use the Data API's
 * own nested routes, so filtering and matching never run over a fully loaded
 * library in the webview.
 */
export async function fetchNoteSummaries(
  f: ListFilter,
): Promise<{ notes: Note[]; hasMore: boolean; nextCursor: string }> {
  const params = new URLSearchParams()
  params.set('summary', '1')
  params.set('limit', String(LIST_PAGE))
  if (f.cursor) params.set('cursor', f.cursor)

  let path: string
  if (f.query) {
    params.set('query', f.query)
    params.set('type', 'note')
    path = `/search?${params}`
  } else if (f.notebookId) {
    path = `/folders/${encodeURIComponent(f.notebookId)}/notes?${params}`
  } else if (f.tagId) {
    path = `/tags/${encodeURIComponent(f.tagId)}/notes?${params}`
  } else {
    params.set('include_conflicts', '1')
    path = `/notes?${params}`
  }

  const page = await apiGet<CursorPage<SummaryDTO>>(path)
  return {
    notes: (page.items ?? []).map(toSummary),
    hasMore: !!page.has_more,
    nextCursor: page.next_cursor ?? '',
  }
}

/** fetchNote loads the full note, body included, for the open editor. */
export async function fetchNote(id: string): Promise<Note> {
  const d = await apiGet<NoteDTO>(`/notes/${encodeURIComponent(id)}`)
  return {
    ...toNote(d),
    tagIds: [],
  }
}

export async function fetchNoteTags(id: string): Promise<Tag[]> {
  const items = await fetchAll<TagDTO>(`/notes/${encodeURIComponent(id)}/tags`)
  return items.map((t) => ({ id: t.id, title: t.title }))
}

export async function createNoteApi(input: { title: string; body: string; notebookId: string }): Promise<Note> {
  const d = await apiSend<NoteDTO>('POST', '/notes', {
    title: input.title,
    body: input.body,
    parent_id: input.notebookId,
  })
  return toNote(d)
}

export async function updateNoteApi(id: string, patch: Record<string, unknown>): Promise<Note> {
  const d = await apiSend<NoteDTO>('PUT', `/notes/${id}`, patch)
  return toNote(d)
}

export async function deleteNoteApi(id: string): Promise<void> {
  await apiSend('DELETE', `/notes/${encodeURIComponent(id)}`, undefined)
}

export async function createNotebookApi(title: string, parentId: string | null): Promise<Notebook> {
  const d = await apiSend<FolderDTO>('POST', '/folders', { title, parent_id: parentId ?? '' })
  return { id: d.id, title: d.title, parentId: d.parent_id || null }
}

export async function createTagApi(title: string): Promise<Tag> {
  return apiSend<TagDTO>('POST', '/tags', { title })
}

export async function setNoteTagsApi(id: string, tagIds: string[]): Promise<void> {
  const current = await fetchNoteTags(id)
  // Add first: a failed attachment must not discard existing associations.
  for (const tagId of new Set(tagIds)) {
    if (!current.some((t) => t.id === tagId)) {
      await apiSend('POST', `/tags/${encodeURIComponent(tagId)}/notes`, { id })
    }
  }
  for (const tag of current) {
    if (!tagIds.includes(tag.id)) {
      await apiSend('DELETE', `/tags/${encodeURIComponent(tag.id)}/notes/${encodeURIComponent(id)}`, undefined)
    }
  }
}
