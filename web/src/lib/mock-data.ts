export type Notebook = {
  id: string
  title: string
  parentId: string | null
}

export type Tag = {
  id: string
  title: string
}

export type Note = {
  id: string
  title: string
  body: string
  /** excerpt is the list-row preview; live summary rows omit the body. */
  excerpt?: string
  notebookId: string
  tagIds: string[]
  createdTime: number
  updatedTime: number
  isTodo: boolean
  todoCompleted: boolean
  todoDue: number | null
  isConflict: boolean
  conflictOriginalTitle: string | null
}

/** SYNTHETIC prototype data — replace with MyGO bindings output in M4. */
export const notebooks: Notebook[] = [
  { id: 'nb-inbox', title: 'Inbox', parentId: null },
  { id: 'nb-jl', title: 'joplin-lite', parentId: null },
  { id: 'nb-jl-specs', title: 'Specs & interop', parentId: 'nb-jl' },
  { id: 'nb-jl-ops', title: 'Release ops', parentId: 'nb-jl' },
  { id: 'nb-reading', title: 'Reading', parentId: null },
  { id: 'nb-travel', title: 'Travel', parentId: null },
]

export const tags: Tag[] = [
  { id: 'tag-e2ee', title: 'e2ee' },
  { id: 'tag-sync', title: 'sync' },
  { id: 'tag-clipper', title: 'clipper' },
  { id: 'tag-idea', title: 'idea' },
  { id: 'tag-todo', title: 'todo' },
]

const H = 3600_000
const D = 24 * H
const NOW = Date.UTC(2026, 9, 16, 9, 30)

export const notes: Note[] = [
  {
    id: '3f2a91c4e5b84d17a6c0f2b9d8e45a11',
    title: 'E2EE interop checklist (M1)',
    body: `# E2EE interop checklist (M1)

The gate: decrypt a real note from my target, edit it here, re-encrypt, and **Joplin decrypts it back**.

## Encoding details that must not drift

- Master key after KeyV1 decryption is used as the **512-char hex string** — PBKDF2 eats those bytes, never hex-decoded ones
- StringV1 plaintext is UTF-16LE, chunked at 64 KiB
- FileV1 for resource blobs; chunk payload is \`{"salt","iv","ct"}\` JSON, base64
- Legacy SJCL packets still appear on old items — dispatch on the header method id

\`\`\`
JED01 | 000022 | 0a <32-hex masterKeyId> | 00019c <chunk> | ...
\`\`\`

> Crypto is a port, not a rewrite. If a detail disagrees with Joplin, Joplin is right.`,
    notebookId: 'nb-jl-specs',
    tagIds: ['tag-e2ee', 'tag-todo'],
    createdTime: NOW - 12 * D,
    updatedTime: NOW - 2 * H,
    isTodo: true,
    todoCompleted: false,
    todoDue: NOW + 2 * D,
    isConflict: false,
    conflictOriginalTitle: null,
  },
  {
    id: 'b7c1d2e94a3f6081c2d4e5f6a7b8c9d0',
    title: 'Dropbox driver notes',
    body: `# Dropbox driver notes

Sync target **7**. Auth lives under \`sync.7.auth\`; the sync dir is the app-folder root.

| call | endpoint |
| --- | --- |
| upload | \`files/upload\` + \`Dropbox-API-Arg\` |
| download | \`files/download\` |
| list | \`files/list_folder\` (+ \`/continue\`) |
| delete | \`files/delete_v2\` |

Item names are Joplin's: \`<32-hex>.md\`, blobs under \`.resource/\`, plus \`info.json\`, \`locks/\`, \`temp/\`.

OAuth: code flow + PKCE, \`token_access_type=offline\` so we get a refresh token.`,
    notebookId: 'nb-jl',
    tagIds: ['tag-sync'],
    createdTime: NOW - 9 * D,
    updatedTime: NOW - 5 * H,
    isTodo: false,
    todoCompleted: false,
    todoDue: null,
    isConflict: false,
    conflictOriginalTitle: null,
  },
  {
    id: '5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d',
    title: 'Conflict: lock semantics on file targets',
    body: `# Lock semantics on file targets

Both sides changed this note — this is the **conflict copy** kept locally.

Locks are files: \`locks/<type>_<clientType>_<clientId>.json\` (numeric, e.g. \`1_1_a91f\`).

- type 1 = SYNC, type 2 = EXCLUSIVE
- client type 1 = desktop, 2 = mobile, 3 = cli
- TTL 3 min, refreshed every 60 s

Refuse to sync while an active EXCLUSIVE lock exists; keep refreshing our SYNC lock during long runs.`,
    notebookId: 'nb-jl-specs',
    tagIds: ['tag-sync', 'tag-e2ee'],
    createdTime: NOW - 6 * D,
    updatedTime: NOW - 40 * H,
    isTodo: false,
    todoCompleted: false,
    todoDue: null,
    isConflict: true,
    conflictOriginalTitle: 'Lock semantics on file targets',
  },
  {
    id: 'e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3',
    title: 'Clipper handshake wiring',
    body: `# Clipper handshake wiring

The extension's flow (port from \`packages/lib/ClipperServer.ts\`):

1. \`GET /ping\` → \`JoplinClipperServer\`
2. \`POST /auth\` → \`{"auth_token":"..."}\`
3. app shows **Accept / Reject**
4. polls \`GET /auth/check?auth_token=…\` → \`waiting\` → \`accepted\` + \`token\`

Port 41184 default; the extension ping-scans 41184–41194. Joplin desktop on the same port means we surface the collision instead of failing silently.`,
    notebookId: 'nb-jl',
    tagIds: ['tag-clipper'],
    createdTime: NOW - 8 * D,
    updatedTime: NOW - 26 * H,
    isTodo: false,
    todoCompleted: false,
    todoDue: null,
    isConflict: false,
    conflictOriginalTitle: null,
  },
  {
    id: '1c2b3a495867564738291a0b1c2d3e4f',
    title: 'Ship v0.1 to the throwaway target',
    body: `# Ship v0.1 to the throwaway target

- [x] cgo-free build
- [x] golden corpus round-trip
- [ ] M1 gate on a real note
- [ ] conflict-note pass

Order matters: corpus before features, locks before write-heavy paths.`,
    notebookId: 'nb-jl-ops',
    tagIds: ['tag-todo'],
    createdTime: NOW - 4 * D,
    updatedTime: NOW - 3 * D,
    isTodo: true,
    todoCompleted: false,
    todoDue: NOW + 5 * D,
    isConflict: false,
    conflictOriginalTitle: null,
  },
  {
    id: '9d8c7b6a5f4e3d2c1b0a9f8e7d6c5b4a',
    title: 'Trains to Basel, November',
    body: `# Trains to Basel, November

ICE 274 — dep 08:14, arr 11:52, platform 12. Seat 47, quiet coach.

Return option: EC 191, 17:06. The 14:03 needs a reservation.

Tickets in the "November" folder of the travel wallet.`,
    notebookId: 'nb-travel',
    tagIds: [],
    createdTime: NOW - 15 * D,
    updatedTime: NOW - 7 * D,
    isTodo: false,
    todoCompleted: false,
    todoDue: null,
    isConflict: false,
    conflictOriginalTitle: null,
  },
  {
    id: '74e65d4c3b2a1908f7e6d5c4b3a29180',
    title: 'Paper — local-first software patterns',
    body: `# Paper — local-first software patterns

Points worth stealing:

1. **Sync is a repair protocol**, not transport — every write is safe alone.
2. Conflicts are *features*: surface them, never resolve them silently.
3. The device copy is the product; the server is a courtesy.

> "The data is yours, on your disk, readable without the vendor." — the whole pitch of joplin-lite in one line.`,
    notebookId: 'nb-reading',
    tagIds: ['tag-idea'],
    createdTime: NOW - 20 * D,
    updatedTime: NOW - 11 * D,
    isTodo: false,
    todoCompleted: false,
    todoDue: null,
    isConflict: false,
    conflictOriginalTitle: null,
  },
  {
    id: '0a1b2c3d4e5f60718293a4b5c6d7e8f9',
    title: 'Quick capture: startup budget',
    body: `# Quick capture: startup budget

Cold start must be **under one second** including SQLite open + FTS warm.

If the webview shell is the slow part, measure before blaming the Go side.`,
    notebookId: 'nb-inbox',
    tagIds: ['tag-idea'],
    createdTime: NOW - 1 * D,
    updatedTime: NOW - 22 * H,
    isTodo: false,
    todoCompleted: false,
    todoDue: null,
    isConflict: false,
    conflictOriginalTitle: null,
  },
]

export const noteCountByNotebook = (notebookId: string): number => {
  const children = notebooks.filter((n) => n.parentId === notebookId).map((n) => n.id)
  const ids = [notebookId, ...children]
  return notes.filter((n) => ids.includes(n.notebookId)).length
}

export const notebookTitle = (id: string): string =>
  notebooks.find((n) => n.id === id)?.title ?? '—'

export const tagTitle = (id: string): string => tags.find((t) => t.id === id)?.title ?? id
