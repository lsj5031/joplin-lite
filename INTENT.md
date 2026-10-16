# joplin-lite — Project Intent & Implementation Plan

**One line:** A small Go/MyGO desktop client that is a **real Joplin peer** — E2EE-capable, multi-target sync, and a Web-Clipper-compatible server.

> Status: evaluated and filled in against the actual `reference/go-joplin` code and
> against Joplin's current source (`SyncTargetRegistry`, `SyncTargetDropbox`, `LockHandler`,
> `Dirnames`, clipper auth spec) on 2026-10-16. Corrections and gaps found in that pass are
> marked **[verified]**, **[correction]**, **[gap]**. Everything else is intent as originally
> written.

## Why

- Joplin's official clients are Electron / React Native: ~200 MB, high RAM, slow start.
- I want a lightweight client that opens instantly, for my own use.
- It must **share one library with Joplin desktop and mobile** (sync), handle the **E2EE** that my target already uses, and **accept clips from the official Web Clipper**.
- My existing notes, phone app, and sync target must keep working untouched.

## What it is

- A desktop app (MyGO, **webview frontend**) that is a **full Joplin client**:
  - a **local store** (SQLite, Joplin's model, plaintext at rest),
  - an **E2EE layer** (decrypt on read, encrypt at sync time),
  - a **sync engine** + per-target **drivers**, and
  - a **clipper server** for the official Joplin Web Clipper extension.
- Reads/writes Joplin's item format **losslessly**, so Joplin and joplin-lite interchange through the sync target.

## Evaluation summary (what the fork gives us and where it falls short)

Verified against `reference/go-joplin`:

| Area | State in fork | Verdict for us |
|---|---|---|
| `internal/e2ee` | JED01 + SJCL + KeyV1/StringV1/FileV1 decrypt, StringV1 encrypt | Keep frozen; one additive gap (`EncryptFile`) |
| `internal/models` | serialize/unserialize for all item types | Keep, but **[gap]** round-trip is not byte-identical and unknown metadata keys are dropped |
| `internal/store` | Joplin schema v49, FTS4, sync/deleted/item_changes tables | Keep; SQLite driver swapped, RAG dropped — **[correction]** search index is now FTS5 (modernc ships no FTS4); local index only, not part of the sync format |
| `internal/sync` | engine, delta, push, reconcile, locks (Joplin-Server only), S3 + Joplin Server drivers | Keep as the model; **[gap]** no conflict notes, weak lock enforcement, resource push unencrypted |
| `internal/clipper` | full Data API on 41184, ping, CORS | Keep; **[gap]** auth auto-accepts (not the real handshake), no `/revisions` |
| Build | **[verified]** does not build here as-is: `mattn/go-sqlite3` + `sqlite-vec` cgo fail without a C toolchain | M0 (cgo-free) is required, not optional |

Corrections to earlier assumptions:

- **[correction]** Sync target IDs (from Joplin's `SyncTargetRegistry`): `0` None, `3` OneDrive,
  **`7` Dropbox**, `8` S3, `9` Joplin Server, `10` Joplin Cloud. Our Dropbox driver returns
  **7** (config keys `sync.7.*`). The fork's `SyncTarget()` IDs 8/9 were right.
- **[correction]** Lock file names on flat-file targets are
  `locks/<type>_<clientType>_<clientId>.json` (numeric — `1_1_<clientId>.json`), **not** the
  Joplin 1.x style `sync_desktop_<clientId>.json`. `type`: 1 = SYNC, 2 = EXCLUSIVE.
  `clientType`: 1 = desktop, 2 = mobile, 3 = cli. TTL 3 min, auto-refresh every 60 s.
  Exclusive locks tie-break by `updatedTime`, then `clientId`. (Joplin `LockHandler`.)
- **[correction]** The clipper handshake returns **`token`** as the field name:
  `POST /auth` → `{"auth_token": "..."}`; `GET /auth/check?auth_token=…` →
  `{"status":"waiting"}` | `{"status":"accepted","token":"API_TOKEN"}` | `{"status":"rejected"}`.
- **Verified truth to keep:** KeyV1's decrypted master key is used downstream as the
  **512-char hex string** (the fork stores `hex.EncodeToString(raw)` and feeds those bytes to
  PBKDF2). This is load-bearing exactly as stated.

## Requirement: E2EE (load-bearing — nothing works without it)

My target is encrypted, so this is M1, not optional. Format details, verified against
`internal/e2ee/e2ee.go` (which is a faithful port and stays frozen):

- **Envelope** `JED01` (`JED` + 2-hex version `01`), ASCII throughout:
  - header: `metadata_length` (6 hex) + `metadata` = `method` (2 hex) + `masterKeyId` (32 hex);
  - then chunks: `chunk_length` (6 hex) + `chunk`.
- **Native chunk payload** (methods 8–10) is one JSON object per chunk:
  `{"salt": b64, "iv": b64, "ct": b64}` — AES-256-GCM, 96-bit IV, 128-bit tag,
  per-chunk key = PBKDF2-HMAC-SHA512(masterKeyBytes, salt, iters, 32).
  - **KeyV1 (8)**: master key; PBKDF2 over the **master password**, 220 000 iters; decrypted
    256-byte key hex-encodes to the 512-char string used everywhere downstream.
  - **StringV1 (10)**: notes/items; PBKDF2 3 iters; plaintext is **UTF-16LE**; chunked at
    64 KiB (`StringV1ChunkSize`).
  - **FileV1 (9)**: resource blobs; PBKDF2 3 iters; raw bytes.
- **Legacy SJCL (methods 1–7)**: SJCL JSON packets (`iv, v, iter, ks, ts, mode, cipher, salt,
  ct, adata`) with AES-CCM and sjcl's quirks (IV clamped to `15-L` bytes, counter in word 3,
  `%XX` unescape for 5a/5b strings). Dispatch on the header method ID; both families coexist.
- **Master key transport**: 256-byte key encrypted under the master password (never leaves
  clients), carried by `info.json` `masterKeys[]` and `master_key` items.
- **Salt for newly generated ciphertext [verified nuance]**: Joplin's native scheme is
  `sha256(168-bit rand ‖ 64-bit ms ‖ 56-bit counter)`, but the salt travels in the packet, so
  decrypt-side interop does not depend on it. Match the scheme when generating anyway — it
  costs nothing and removes a "why is ours different" class of bug.
- **Checksum** of a decrypted master key: `sha256hex(decryptedHexString)`.
- **Interop is unforgiving**: treat crypto as a port with tests, never as an improvisation.

**[gap]** `e2ee.Service` has `DecryptFile` but no `EncryptFile` — push of resource blobs needs
it. Add it **additively** (new file in `internal/e2ee`, e.g. `encrypt_file.go`): the freeze
protects existing code from edits; a new, test-covered encoder that round-trips against the
existing decoder is in the spirit of "crypto is a port". Joplin must decrypt the output in the
M1 gate before it is trusted.

## Requirement: sync

Coexist with Joplin desktop/mobile on the same target.

- **Delta sync** driven by `updated_time` and per-item, per-target `sync_time` (`sync_items`)
  — exactly the fork's push query (`updated_time > si.sync_time`, or missing sync_items row).
  Flat-file targets have no delta API: the driver emulates it by listing the target
  (`/api/items/root:/:/delta` synthesized like `s3_backend.getDelta`), and
  `PullMissingItems` reconciliation + pruning covers missed changes.
- **Deletion propagation** through `deleted_items` (fork already does this: push deletes the
  remote object; remote deletions found by the listing diff are pruned locally).
- **Conflicts**: both sides changed → Joplin-style conflict note (never lose data).
  **[gap]** the fork currently overwrites local changes on pull. Required behavior: when a
  pulled remote item differs and the local copy was modified since its `sync_time`, keep the
  local version as a conflict note (`is_conflict=1`, `conflict_original_id`, `deleted_time` set
  on the note copy per Joplin convention) and take the remote version as canonical. Same
  treatment for resources.
- **Locks** on flat-file targets: file locks in `locks/` (format above); refresh at 60 s
  against a 3-min TTL; refuse to sync while an active EXCLUSIVE lock exists; while we hold a
  SYNC lock, refresh it during long syncs. On Joplin Server the fork's `/api/locks` client
  already implements built-in locks — keep it for the Joplin Server driver.
  **[gap]** the engine currently logs and continues when locking fails; that must become a
  refusal for file targets.
- **Target version check** via `info.json` (`version`, `appMinVersion`): refuse to sync if
  `version` is newer than we support. **[gap]** the fork reads `info.json` but neither
  enforces the version nor ever writes `info.json` — M2 must write it (e2ee flag, active
  master key, master keys) when those change.

## Requirement: sync targets

- One shared engine; **drivers** behind the existing 7-method interface — **[verified]** the
  exact contract the engine speaks:

  ```go
  // internal/sync/backend.go
  type SyncBackend interface {
      Authenticate() error
      IsAuthenticated() bool
      AcquireLock() (interface{}, error)
      ReleaseLock(lock interface{}) error
      Get(path string) ([]byte, error)   // Joplin-Server-style API paths
      Put(path string, content []byte) error
      Delete(path string) error
      SyncTarget() int                   // 7 = Dropbox, 8 = S3, 9 = Joplin Server
  }
  ```

  The engine calls `Get/Put/Delete` with Joplin Server API shapes —
  `/api/items/root:/<item_name>:/content`, `/api/items/root:/:/delta[?cursor=…]`,
  `/api/items/root:/:/children[?cursor=…]` — and each driver translates them (the fork's
  `s3_backend.go` `parsePath` is the model). Item names: `<32-hex-id>.md`, resources as
  `.resource/<32-hex-id>`, plus `info.json`, `locks/`, `temp/`.
- **v1 driver: Dropbox** (what I actually use) — **my own Dropbox OAuth app**.
  **[verified]** Dropbox is sync target **7**; Joplin stores auth under `sync.7.auth`; the
  Dropbox sync dir is the app-folder root (`appDir = ''`). Driver = `internal/sync/dropbox_backend.go`,
  modeled on `s3_backend.go`, over Dropbox API v2:
  - `files/upload` (POST `content.dropboxapi.com`, `Dropbox-API-Arg` header, raw body),
  - `files/download` (same arg header), `files/list_folder` + `files/list_folder/continue`
    (delta/children emulation), `files/delete_v2`, `files/get_metadata`;
  - OAuth2 authorization-code with **PKCE**, `token_access_type=offline` for a refresh token,
    loopback redirect on `127.0.0.1`; scopes `files.content.read files.content.write`
    (+ `files.metadata.read` if listing demands it — verify in the app console);
  - Dropbox app registration: scoped app, **App folder** permission; my app key/secret;
    auth token persisted in our config (`sync.7.auth` JSON), refreshed on expiry.
- Then, one at a time: **local folder, WebDAV/Nextcloud, S3, Joplin Server/Joplin Cloud,
  OneDrive**. "All of them" is a roadmap, not a milestone.

## Requirement: browser extension

Clipper server compatible with the official Joplin Web Clipper:

- Bind loopback, default **41184** (extension ping-scans 41184–41194).
- `GET /ping` → `JoplinClipperServer` — **[verified]** the fork already matches this, plus a
  `/health` probe.
- Auth handshake (spec: joplinapp.org/help/dev/spec/clipper_auth), **[verified]** exact shape:
  - `POST /auth` → `{"auth_token": "AUTH_TOKEN"}` (server generates and remembers it);
  - app shows **Accept/Reject** (a real prompt — **[gap]** the fork auto-accepts in
    `clipper/auth.go` and must not);
  - `GET /auth/check?auth_token=…` → `{"status":"waiting"}` →
    `{"status":"accepted","token":"API_TOKEN"}` or `{"status":"rejected"}`;
  - the API token is also copyable from settings (the second, documented auth path).
- The **Data API**: `/notes`, `/folders`, `/resources` (multipart), `/tags`, `/note_tags`,
  `/search`, `/events`, `/revisions`. **[gap]** the fork has notes/folders/tags/resources/
  search/events (and `/tags/{id}/notes`) but no `/revisions`; pin the exact route set the
  extension calls against Joplin's `packages/lib/ClipperServer.ts` and match it.
- Permissive **CORS + preflight** (fork already does this via chi/cors).
- **Cannot coexist with Joplin desktop on port 41184** — surface that clearly in the UI and
  logs (bind failure = explicit "Joplin desktop is using this port" message).
- After a clip lands, trigger sync (fork already wires `SyncTrigger`).

## What it is not (v1)

- **Not a plugin host**, **not mobile**.
- Not a sync *server* (we are a client; Joplin Server/Cloud is a target, not a thing we host).

## Core principles

1. **Losslessness first** — re-serialized items must be byte-identical; unknown props preserved.
2. **Never lose data** — conflicts become conflict notes, never overwrites.
3. **Crypto is a port, not a rewrite** — validated against Joplin, with interop tests.
4. **MyGO is only the shell** — the backend stays a headless, independently testable Go service.
5. **`\n`, UTF-8, ISO-8601 UTC in files; unix ms in memory.**

## Foundation (decided)

joplin-lite is a **fork of `jescarri/go-joplin`** (cloned at `reference/go-joplin`). It is
**AGPL-3.0-or-later**, so joplin-lite is too. Keep upstream attribution and add a NOTICE.

We keep and build on:

- `internal/e2ee` — JED formats + KeyV1/StringV1/FileV1 + SJCL. **Do not modify** (additive
  `EncryptFile` only). Note: it imports the `otel` API for spans — we cut the
  `internal/telemetry` package (metrics/middleware/exporters) but keep the tiny `otel`/`otel/trace`
  API dependency so `e2ee` stays untouched; spans no-op without an exporter.
- `internal/models` — item serialize/unserialize (with the losslessness work below).
- `internal/store` — local SQLite.
- `internal/sync` — engine, delta, locks, reconcile. Drivers sit behind `SyncBackend`.
- `internal/clipper` — the 41184 server (ping / auth / Data API) already exists.

We cut for v1: `internal/mcp`, `internal/rag`, `internal/telemetry`, `store/rag.go`,
`store/db.go`'s `InitRAG` + `sqlite-vec` init, and the MCP mount in `clipper/server.go`
(drop the `mcp`/RAG parameters from `NewServer`). **[M0 note]** The mutation `Policy`
moved *intact* into `internal/clipper` (nil-deny semantics preserved) rather than dying
with `internal/mcp` — the clipper handlers depend on it; `NewServer` is now
`(db, apiToken, apiKey, syncer, policy)`. `sync_state.go`'s `DeleteNoteRAGData` hook
went with `store/rag.go`.

**[gap] Losslessness work (M0, before anything trusts serialization):** `models/serialize.go`
today is not byte-identical — `writeMetadata` emits only known keys (unknown metadata lines are
dropped), `order` is formatted through the time formatter, floats are fixed at 8 decimals, and
trailing whitespace is trimmed on parse. Plan:
- keep the **raw item text** for anything pulled and unmodified, and push those exact bytes
  back (losslessness by construction);
- when we do re-serialize, emit unknown metadata lines verbatim in their original position;
- lock the rest with a **golden corpus**: real items from my target (note, todo, folder, tag,
  note-tag, resource, master key, conflict note) asserting `Serialize(Deserialize(x)) == x`
  byte-for-byte. This corpus is the format gate of every later milestone.

**Our work:**1. Go **cgo-free**: drop `rag`/`mcp`, swap `mattn/go-sqlite3` → `modernc.org/sqlite` (`CGO_ENABLED=0`; no MSVC; matches MyGO). **[correction — verified in M0]** modernc's build ships `SQLITE_ENABLE_FTS5` only — **no FTS4** — so `notes_fts` migrated to FTS5 external content (`content='notes'`, canonical `'delete'`-command triggers); `search.go`'s join is unchanged and the index is local-only (not part of the sync format). DSN pragmas are the modernc `_pragma=` form (`_pragma=journal_mode(WAL)` etc.); WAL verified by the M0 smoke test.
2. A **Dropbox** `SyncBackend` + Dropbox OAuth (details above).
3. More drivers later behind the same interface.
4. A **MyGO** shell + typed bindings + web UI.

## Architecture

```
joplin-lite/                    # Go module (fork of go-joplin, AGPL-3.0-or-later)
  cmd/                          # CLI: serve, sync, clip, config (headless surface stays)
  internal/e2ee/                # frozen crypto port (+ additive EncryptFile)
  internal/models/              # item models + lossless serialization
  internal/store/               # SQLite (modernc), Joplin schema v49
  internal/sync/                # engine + SyncBackend + dropbox_backend.go, s3, joplin server
  internal/clipper/             # 41184 server (auth handshake + Data API)
  internal/config/              # YAML config + env expansion
  service/                      # MyGO bound services (typed IPC for our UI)
  web/                          # UI: TanStack Start (latest) + React + TypeScript [user-directed]
  reference/go-joplin/          # upstream, kept for reference/patches
  INTENT.md  PRODUCT.md  DESIGN.md
```

UI stack decision: the prototype and product UI are **TanStack Start + React + TS** in `web/`
(user direction). All data access goes through a thin `lib/api` layer with two implementations
— mock (prototype, today) and MyGO typed bindings (M4) — so components stay shell-agnostic.
Open item for M4: whether the shipped webview loads Start's static SPA build (likely) or the
Start server runs inside the Go process; the mock/bindings split keeps both open. Editor
stack (CodeMirror 6, markdown-it, KaTeX, DOMPurify) is unchanged.

## Success criteria

- **Format gate:** every item round-trips byte-identically (golden corpus green).
- **E2EE gate:** decrypt a real note from my target, edit, re-encrypt, and **Joplin decrypts it back**.
- **Sync gate:** edit here → sync → Joplin desktop shows it, and the reverse, with no loss;
  a deliberate double-edit produces a conflict note, not a silent overwrite.
- **Clipper gate:** the official extension clips a page in, and it survives a sync round-trip.- Footprint: single cgo-free binary, starts in under a second. **[measured 2026-10-17 — windows/amd64, Go 1.26.4, modernc.org/sqlite v1.60.1, `-ldflags="-s -w"`] 24,385,024 bytes (23.3 MiB); startup (`--help`) 67–94 ms over 3 runs.** The "~7 MB" aspiration is dead — the real number is ~24 MB; decide at M5 (accept, strip, or reconsider the driver).

## Milestones

**M0 — headless, cgo-free, lossless baseline**
- Cut `rag`/`mcp`/`telemetry`; swap to `modernc.org/sqlite`; `go build ./...` and
  `go test ./...` green on Go 1.26 with `CGO_ENABLED=0` (today's build fails without a C
  toolchain — that failure is the starting point).
- Golden-corpus round-trip tests + the raw-passthrough/unknown-key serialization fix.
- Store smoke test: WAL, FTS5 search, resource blob round-trip through the new driver.
- Record binary size and startup time.
- **Gate:** format gate green; all existing round-trip + e2ee tests green.
- **Progress (2026-10-17):** bullets 1, 3, 4 done — the fork lives at the repo root (module `github.com/lsj5031/joplin-lite`), `CGO_ENABLED=0 go build ./...` and `go test ./...` green (clipper, config, e2ee, store, sync), smoke test added (`internal/store/db_smoke_test.go`: WAL + FTS5 insert/update/delete round-trip + resource blob), size/startup recorded under Success criteria. **Remaining: the golden corpus + raw-passthrough serialization fix — the format gate — which is the rest of M0.**

**M1 — E2EE interop**
- Load master keys from the master password (existing `DecryptPulledItems` flow), verify the
  KeyV1 checksum path end-to-end against my real target.
- Add additive `EncryptFile` (FileV1) with round-trip tests.
- **Gate (the E2EE gate):** decrypt a real note from my target, edit, re-encrypt here,
  sync, and **Joplin desktop decrypts and displays it**. Repeat for a resource blob.

**M2 — Dropbox sync**
- Dropbox OAuth app + PKCE flow + token refresh; `dropbox_backend.go` translating the API
  paths (model: `s3_backend.go`); `SyncTarget() == 7`.
- File-based locking (`locks/1_1_<clientId>.json`, 60 s refresh, EXCLUSIVE refusal),
  `info.json` read *and* write + version enforcement.
- Conflict-note pass (never-lose-data), encrypted resource push (metadata + blob).
- **Gate (the sync gate):** on a throwaway Dropbox app folder — edit here → sync → Joplin
  desktop shows it, and the reverse; double-edit yields a conflict note; deletion propagates
  both ways; a Joplin-desktop-held EXCLUSIVE lock blocks our sync.

**M3 — clipper server**
- Real auth handshake (POST /auth + Accept/Reject prompt + /auth/check polling), API token in
  settings, route set matched to Joplin's `ClipperServer.ts` incl. `/revisions`, explicit
  port-collision messaging.
- **Gate (the clipper gate):** the official extension clips a page (simplified + full clip),
  the note lands with its resource, and it survives a sync round-trip into Joplin desktop.

**M4 — MyGO shell + UI**
- `service/` typed bindings; the `web/` UI (prototype → product) wired to bindings; read/write
  notes, notebooks, tags, search, settings (sync target, master password, clipper token,
  conflict notes view).
- Requires Go 1.27 + Bun per MyGO's requirements (pin the MyGO version).
- **Gate:** the full loop works from the UI alone: edit → sync → clipper → conflict visible.

**M4a — UI prototype (this repo, in progress)**
- TanStack Start + React prototype in `web/` with synthetic (clearly labeled) mock data:
  three-pane workspace, note list, markdown editor/preview, search, sync/E2EE status,
  settings, empty/conflict states. Establishes the visual system later recorded in DESIGN.md.
- **Gate:** prototype builds, all listed states reachable, mock data labeled synthetic.

**M5 — packaging**
- `mygo build`, installer, autostart-less single binary; measure footprint against the
  success criteria; AGPL notices shipped.

## Risks

- **E2EE interop** — the top risk; a single encoding detail silently breaks all data. Mitigate
  by porting proven code and the M1 real-target gate. The hex-string master key quirk and
  UTF-16LE StringV1 encoding are the two details most likely to be "helpfully" refactored.
- **Losslessness** — the fork's serializer is not byte-identical today; a re-serialization
  that loses unknown fields corrupts notes for every device. Raw-passthrough + golden corpus
  first, features later.
- **Driving a shared library** — a bad sync can corrupt notes for every device. Test on a
  throwaway Dropbox app folder first; locks and conflict notes before write-heavy features.
- **Dropbox OAuth** — needs my own app registration and possibly review; app-folder scope
  keeps blast radius small.
- **Port 41184** collides with Joplin desktop; both cannot run at once.
- **MyGO is ~3 weeks old** — pin the version; isolate it to the shell layer (already the plan:
  `service/` is the seam).
- **Footprint** — cgo-free SQLite (modernc) inflates the binary; "~7 MB" may become
  ~15–20 MB. Measure, then decide: accept, strip, or reconsider the driver.

## Open questions (verify on real targets, not in code)

1. Joplin's exact float formatting for `latitude/longitude/altitude` and `order` in serialized
   items — the golden corpus answers this; fix `serialize.go` to match reality, whatever it is.
2. Exact clipper route set + query semantics the extension exercises (port from
   `packages/lib/ClipperServer.ts`).
3. Whether current Joplin ever writes `locks/` with legacy `sync_desktop_*` names (old
   targets) — accept both on read, write numeric only.
4. Dropbox listing pagination + `has_more` behavior for very large libraries (delta emulation).
5. Whether the webview ships the Start SPA build or a local Start server (M4 decision).
