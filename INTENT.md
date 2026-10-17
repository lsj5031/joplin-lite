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
| `internal/sync` | engine, delta, push, reconcile, locks (Joplin-Server only), S3 + Joplin Server drivers | Keep as the model; **[gap]** no conflict notes, weak lock enforcement, resource push unencrypted — all three **[done 2026-10-17]** (see Conflicts, Locks, E2EE notes below) |
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
  **[update 2026-10-17]** Local deletes now queue `deleted_items` from the store's delete paths
  (one row per synced target, incl. note-tag links and resource blob cleanup on push);
  both-ways propagation on a real target stays an M2 gate item.
- **Conflicts**: both sides changed → Joplin-style conflict note (never lose data).
  **[gap]** the fork currently overwrites local changes on pull. Required behavior: when a
  pulled remote item differs and the local copy was modified since its `sync_time`, keep the
  local version as a conflict note (`is_conflict=1`, `conflict_original_id`, `deleted_time` set
  on the note copy per Joplin convention) and take the remote version as canonical. Same
  treatment for resources.
  **[update 2026-10-17 — done]** `internal/sync/conflicts.go`, wired into the pull path
  (`delta.go`, `reconcile.go`) and tested through the local-folder rehearsal
  (`internal/sync/conflicts_test.go`). Convention **[verified]** against `packages/lib`
  (`Note.createConflictNote`, `Note.mustHandleConflict`, `Resource.createConflictResourceNote`/
  `duplicateResource`, `Folder.markNotesAsConflict`, `handleConflictAction`): the conflict note
  is a copy of the **local** note under a new id with `is_conflict=1`,
  `conflict_original_id=<original id>` (empty when the remote copy was deleted), `is_shared`/
  `share_id` cleared — **title and parent_id are NOT changed** (the Conflicts notebook is
  virtual: fixed id `c04f1c7c04f1c7c04f1c7c04f1c7c04f` listing `is_conflict=1` notes), and
  `deleted_time` is inherited like every other field. A note conflict is raised only when
  title/body differ (or cipher text is involved — `Note.mustHandleConflict`); resources
  conflict when `blob_updated_time` differs, resolved by duplicating the resource (blob
  copied, share fields cleared) plus an `Attachment conflict: "…"` pointer note in a real
  "Conflicts (attachments)`" folder. Remote delete + local changes → conflict copy first,
  then delete; a folder deleted remotely while still holding notes marks them `is_conflict`.
  Folders/tags/note-tags follow Joplin's `ItemConflict`: the remote version wins, no copy.
  Staleness rules keep at-least-once redelivery from clobbering pending local edits (the
  fork's overwrite-on-pull bug used to silently drop a local edit at the next sync's cursor
  boundary). Residual edges: a blob-only delta window can yield two conflict copies (data is
  never lost); items never synced to this target take the remote version (no sync point to
  detect "changed since" — pinned by `delta_test`); Joplin's auto-merge (base_body/base_title
  three-way merge) is not ported.
- **Locks** on flat-file targets: file locks in `locks/` (format above); refresh at 60 s
  against a 3-min TTL; refuse to sync while an active EXCLUSIVE lock exists; while we hold a
  SYNC lock, refresh it during long syncs. On Joplin Server the fork's `/api/locks` client
  already implements built-in locks — keep it for the Joplin Server driver.
  **[update 2026-10-17 — done]** `internal/sync/filelock.go` implements the flat-file
  protocol for LocalFolder, Dropbox **and S3** (shared `lockStore` interface; S3 gets it
  over its object store and the item listings now skip `locks/`, `temp/`, `info.json`).
  The engine now **refuses** the sync on any lock failure for file targets
  (2/7/8) — an active foreign EXCLUSIVE lock surfaces `ErrExclusiveLock` before anything
  touches the target. Semantics **[verified]** against `LockHandler.ts`: activity is judged
  by the file's updated time on the target (3-min TTL; the JSON body's `updatedTime` is not
  authoritative), a SYNC lock is shared, our own active lock is refreshed instead of
  refused, and among active EXCLUSIVE locks the winner is the oldest `updatedTime`, then
  smallest `clientId` (Joplin `activeLock` sorts ascending and takes the first). A refused
  acquire deletes our own lock file again (Joplin's catch → releaseLock). Refresh runs
  every 60 s; an expired lock is never re-acquired (`ErrLockExpired`).
- **Target version check** via `info.json` (`version`, `appMinVersion`): refuse to sync if
  `version` is newer than we support. **[update 2026-10-17 — done]** `syncinfo.go` enforces
  both (`ErrSyncTargetUnsupported` refuses the sync; missing `version` is invalid, matching
  Joplin's `fetchSyncInfo`) and `PushSyncInfo` writes `info.json` when local master keys /
  active key are missing on the target. Facts **[verified]** against `syncInfoUtils.ts`:
  the file is tab-indented JSON; `e2ee`/`activeMasterKeyId` are `{value, updatedTime}`
  pairs (`e2ee.value` is a boolean); `version` is Joplin's hidden `syncVersion` (**3**);
  `appMinVersion` is the format floor (Joplin 3.7 writes **3.7.0** — we publish the same);
  master keys are full entities **including `content`/`checksum`**; unknown keys (`ppk`,
  `noteLockKey`, `revisionService*`) must survive a rewrite. The write is therefore a
  merge: remote entries win on id collision and remote values win when we have no change
  timestamp for ours (Joplin `mergeSyncInfos` prefers the newer `updatedTime`; "no
  timestamp" must lose — otherwise a stale local value would disable the target's E2EE).

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
  **[update 2026-10-17 — implemented]** `internal/sync/dropbox_backend.go` (API v2:
  `files/upload`, `files/download`, `files/list_folder`+`continue` for the delta/children
  emulation, `files/delete_v2`) + `dropbox_auth.go`/`dropbox_flow.go` (PKCE flow with
  loopback redirect, default `http://127.0.0.1:41723` — must be registered in the app
  console) + `cmd/dropbox-auth`. `SyncTarget() == 7`, flat-file locks via `locks/`.
  Decisions: scopes are `files.content.read files.content.write files.metadata.read`
  (the delta emulation lists the app folder — `files.metadata.read` is needed for
  `files/list_folder`; verify in the app console); the token is persisted at
  `<dataDir>/dropbox_token.json` (0600) with a `sync.7.auth` JSON bootstrap (we do not
  rewrite Joplin's settings.json); refresh runs when the access token is within 60 s of
  expiry (and once on a 401). Config: `sync.7.appKey` / `sync.7.appSecret`
  (`GOJOPLIN_DROPBOX_APP_KEY`/`GOJOPLIN_DROPBOX_APP_SECRET`); the app secret is optional
  (PKCE public client). Tested against a fake Dropbox API (round-trip incl.
  `.resource/<id>` name escaping, delta paging + cursor boundary, lock protocol, token
  refresh, full PKCE loopback with verifier↔challenge linkage). **Gate still needs the real
  app + Dropbox.**
- Then, one at a time: **local folder, WebDAV/Nextcloud, S3, Joplin Server/Joplin Cloud,
  OneDrive**. "All of them" is a roadmap, not a milestone.

## Requirement: browser extension

Clipper server compatible with the official Joplin Web Clipper:

- Bind loopback, default **41184** (extension ping-scans 41184–41194).
- `GET /ping` → `JoplinClipperServer` — **[verified]** the fork already matches this, plus a
  `/health` probe.
- Auth handshake (spec: joplinapp.org/help/dev/spec/clipper_auth), **[verified]** exact shape:
  - `POST /auth` → `{"auth_token": "AUTH_TOKEN"}` (server generates and remembers it);
  - app shows **Accept/Reject** (a real prompt — **[gap closed 2026-10-17]** `clipper/auth.go`
    no longer auto-accepts: a pending request stays `waiting` until an `AuthPrompt` decides
    (console y/N prompt in `serve` today; the M4 UI hooks `Server.ResolveAuth`/
    `PendingAuth`));
  - `GET /auth/check?auth_token=…` → `{"status":"waiting"}` →
    `{"status":"accepted","token":"API_TOKEN"}` or `{"status":"rejected"}`;
  - the API token is also copyable from settings (the second, documented auth path) —
    `serve` logs it at startup in the meantime.
  **[verified 2026-10-17 against `routes/auth.ts` + `Api.ts` checkToken_]**: `GET
  /auth/check?token=<x>` → `{"valid": true|false}` is a second mode; an unknown
  `auth_token` is an error (we return 400 `Invalid auth token`); the Data API authenticates
  with a **`?token=` query parameter** (not a header — our old Bearer-only gate would have
  failed the extension) with an open whitelist (`GET ping`, `GET/POST auth`, `GET
  auth/check`) — `GET /ping` had been wrongly locked; `Authorization: Bearer` is kept for
  our own tooling. Pairing endpoints additionally reject **web-page Origins** (403,
  `ClipperServer.ts` `isAllowedPairingOrigin`) — the extension's `chrome-extension://`
  origin and headerless native clients pass.
- The **Data API**: `/notes`, `/folders`, `/resources` (multipart), `/tags`, `/note_tags`,
  `/search`, `/events`, `/revisions`. **[gap closed 2026-10-17]** route set pinned against
  Joplin's `Api.ts` (`resourceNameToRoute_`: ping, notes, folders, tags, resources,
  master_keys, search, auth, events, revisions, services, mcp) and `/revisions` added.
  **[update 2026-10-17 — revisions are real now]** pulled revision items (type 13) are
  persisted: an index table (`revisions`) for listing/filtering plus the **exact item bytes**
  in `item_raw` — the entity served by `/revisions` is the raw item's metadata (diff fields
  `title_diff`/`body_diff`/`metadata_diff` and all) with API typing per the serialization
  rules (ISO times → unix-ms numbers). There is deliberately **no revision serializer**:
  re-serializing a diff-carrying item risks mangling it, so losslessness stays by
  construction (raw passthrough). `DELETE /revisions/{id}` queues the remote delete like
  every other item. **[update 2026-10-17 — revision envelopes decrypt now]** the `revisions`
  table carries `encryption_cipher_text`/`encryption_applied` (additive migration) and the
  decrypt pass handles revision envelopes like notes (`GetEncryptedItems` + `sync/decrypt.go`,
  pinned end-to-end by `internal/sync/revisions_test.go`), so `/revisions` serves encrypted
  revisions decrypted. Fixing it surfaced that `migrate()`'s `version >= 49` early-return
  skipped `ensureSidecarSchema` on existing databases — sidecar columns are now applied on
  every open (`internal/store/revisions_test.go` pins an upgraded v49 database).
  `nounce` request-dedup and `master_keys`/`services` routes are not ported (the extension
  doesn't need them for clipping).
- Permissive **CORS + preflight** (fork already does this via chi/cors).
- **Cannot coexist with Joplin desktop on port 41184** — surface that clearly in the UI and
  logs (bind failure = explicit "Joplin desktop is using this port" message).
  **[done 2026-10-17]** `clipper.Serve` maps an address-in-use bind failure to an explicit
  "Joplin desktop is probably running there" error (tested, incl. the Windows WSAEADDRINUSE
  wording).
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
trailing whitespace is trimmed on parse. Plan:- keep the **raw item text** for anything pulled and unmodified, and push those exact bytes back (losslessness by construction) —  **[M0 done]** `item_raw` sidecar table (`internal/store/raw.go`) + `RawItem` (`internal/models/rawitem.go`); `sync/delta.go` captures raw bytes on pull, `sync/push.go` passes them through when `updated_time` is unchanged; **[2026-10-17]** after the decrypt pass the raw copy is replaced with the **decrypted plaintext** (`sync/decrypt.go` `saveDecryptedRaw`) — keeping the pulled envelope there made the merge path re-serialize an envelope as a plaintext item and drop unknown keys from inside the cipher text (pinned by `TestE2EE_EditKeepsUnknownKeys`);
- when we do re-serialize, emit unknown metadata lines verbatim in their original position — **[M0 done]** `RawItem.SerializeWithOverrides` keeps unknown keys (e.g. Joplin's `is_locked`, `extracted_resource_ids`, which our model lacks) verbatim at their original positions and re-emits known keys only when the value changed;
- lock the rest with a **golden corpus**: real items from my target (note, todo, folder, tag, note-tag, resource, master key, conflict note) asserting `Serialize(Deserialize(x)) == x` byte-for-byte. This corpus is the format gate of every later milestone. — **[M0 done, synthetic]** `internal/models/testdata/golden/` (14 fixtures incl. empty-body, unicode, escaping quirks, unknown keys, both envelope forms) asserting raw/merge/typed byte-identical round-trips; **M1 replaces/extends these with real items from the target — that was the corpus's intent.**

**Serialization facts (verified 2026-10-17 against `BaseItem.ts`):** sections are `title`, `body` (omitted when empty), metadata, joined by blank lines, no trailing newline (Joplin's own parser chokes on one); metadata values escape only literal `\n`/`\r` sequences and real newlines/CRs — bare backslashes are untouched (`note_paths.md` locks the quirk); `type_` is last; the encrypted envelope is the keep-keys reduced item (`id`, `note_id`, `tag_id`, `parent_id`, `share_id`, `updated_time`, `deleted_time`, `type_`, `master_key_id` + `encryption_applied`, `encryption_cipher_text`) as a metadata-only block. Known model gap: `order` is int64 here; a fractional Joplin order would truncate through the typed path (raw-passthrough covers unmodified items) — widen to float64 if a real item shows one.

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
- **Clipper gate:** the official extension clips a page in, and it survives a sync round-trip.
- **Footprint:** single cgo-free binary, starts in under a second. **[measured 2026-10-17 — windows/amd64, Go 1.26.4, modernc.org/sqlite v1.60.1, `go build -trimpath -ldflags="-s -w"`] the default (Dropbox / Joplin Server / local-folder) build is 16,351,232 bytes (15.6 MiB); the optional `-tags s3` build is 24,591,872 bytes (23.5 MiB) because it links aws-sdk-go-v2. Startup (`--help`) 74–133 ms over 3 runs (same code path as the M2 measurement).** `-trimpath` is part of the command: without it the byte count varies with the checkout path. The "~7 MB" aspiration is dead; ~16 MB is the default number and S3 is an opt-in build at M5.

## Milestones

**M0 — headless, cgo-free, lossless baseline**
- Cut `rag`/`mcp`/`telemetry`; swap to `modernc.org/sqlite`; `go build ./...` and
  `go test ./...` green on Go 1.26 with `CGO_ENABLED=0` (today's build fails without a C
  toolchain — that failure is the starting point).
- Golden-corpus round-trip tests + the raw-passthrough/unknown-key serialization fix.
- Store smoke test: WAL, FTS5 search, resource blob round-trip through the new driver.
- Record binary size and startup time.
- **Gate:** format gate green; all existing round-trip + e2ee tests green.
- **Done (2026-10-17), gate green:** all bullets — fork at repo root (module `github.com/lsj5031/joplin-lite`), `CGO_ENABLED=0 go build ./...` / `go test ./...` green (clipper, config, e2ee, models, store, sync), store smoke test (`internal/store/db_smoke_test.go`), golden corpus + raw-passthrough (`internal/models/testdata/golden/`, `internal/models/rawitem.go`, `item_raw` sidecar through `sync/delta.go` + `sync/push.go`), size/startup recorded under Success criteria. Corpus fixtures are synthetic-but-canonical; M1 extends them with real target items.

**M1 — E2EE interop**
- Load master keys from the master password (existing `DecryptPulledItems` flow), verify the
  KeyV1 checksum path end-to-end against my real target.
- Add additive `EncryptFile` (FileV1) with round-trip tests. **[done 2026-10-17]**
  `internal/e2ee/encrypt_file.go` + `encrypt_file_test.go` — FileV1 round-trips against the
  frozen `DecryptFile` (128 KiB chunks per Joplin's `chunkSize(FileV1)`, Joplin salt shape);
  `e2ee.go` untouched.
- **Gate (the E2EE gate):** decrypt a real note from my target, edit, re-encrypt here,
  sync, and **Joplin desktop decrypts and displays it**. Repeat for a resource blob.

**M2 — Dropbox sync**
- Dropbox OAuth app + PKCE flow + token refresh; `dropbox_backend.go` translating the API
  paths (model: `s3_backend.go`); `SyncTarget() == 7`. **[done 2026-10-17 — see the
  sync-targets section; code complete and tested against a fake Dropbox API]**.
- File-based locking (`locks/1_1_<clientId>.json`, 60 s refresh, EXCLUSIVE refusal),
  `info.json` read *and* write + version enforcement. **[done 2026-10-17 — see the Locks
  and Target-version sections; `internal/sync/filelock_test.go`, `syncinfo_test.go`]**.
- Conflict-note pass (never-lose-data) **[done 2026-10-17 — see the Conflicts section above;
  the local-folder rehearsal covers the double-edit and remote-delete legs]**,
  encrypted resource push (metadata + blob) **[done 2026-10-17]**: `pushResources` encrypts
  the metadata (StringV1 envelope) and the blob (FileV1 via `EncryptFile`), the pushed
  plaintext carries `encryption_blob_encrypted: 1`, and the decrypt side now handles
  resource metadata envelopes (`GetEncryptedItems` + `decryptItem`) with the blob pass
  already decrypting JED01 blobs (`TestE2EE_ResourcePushPullRoundTrip`).
- **Gate (the sync gate):** on a throwaway Dropbox app folder — edit here → sync → Joplin
  desktop shows it, and the reverse; double-edit yields a conflict note; deletion propagates
  both ways; a Joplin-desktop-held EXCLUSIVE lock blocks our sync.

**M3 — clipper server**
- Real auth handshake (POST /auth + Accept/Reject prompt + /auth/check polling), API token in
  settings, route set matched to Joplin's `ClipperServer.ts` incl. `/revisions`, explicit
  port-collision messaging. **[done 2026-10-17 — code complete, `internal/clipper/auth_test.go`
  covers the pairing flow, token matrix, origin protection, /revisions and the collision
  message; see the browser-extension section for the verified details]**. Remaining before
  the gate: "API token in settings" is a console log line until the M4 settings UI exists.
- **Gate (the clipper gate):** the official extension clips a page (simplified + full clip),
  the note lands with its resource, and it survives a sync round-trip into Joplin desktop.

**M4 — MyGO shell + UI**
- `service/` typed bindings; the `web/` UI (prototype → product) wired to bindings; read/write
  notes, notebooks, tags, search, settings (sync target, master password, clipper token,
  conflict notes view).
- **[update 2026-10-17 — `service/` seam landed]** `service/` is the typed Go API the MyGO
  bindings wrap: notes (CRUD, list/filter), notebooks (tree/list/CRUD), tags (link/unlink,
  notes-by-tag, tags-by-note), search, the conflict-notes view, and settings
  (`GetSetting`/`SetSetting` over `key_values`) — pinned by `service/service_test.go`.
  Note lists follow Joplin's `include_conflicts` semantics (conflict copies excluded from
  folder/all-notes views, visible in the Conflicts view); the Data API's `GET /notes` and
  `GET /folders/{id}/notes` honor the `include_conflicts` query parameter for the same
  reason. Still missing for the gate: bindings for the runtime pieces (sync target, master
  password, clipper token) — those need the config + sync engine, not just the store.
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
- **Footprint** — cgo-free SQLite (modernc) inflates the binary; "~7 MB" became
  ~16 MB. S3/aws-sdk is now behind `-tags s3`, so the Dropbox-first default stays
  at ~16 MB; decide at M5 whether that is acceptable or whether to strip further.

## Open questions (verify on real targets, not in code)

1. ~~Joplin's exact float formatting for `latitude/longitude/altitude` and `order`~~ **[resolved 2026-10-17 from `packages/lib/models/BaseItem.ts`]**: `order`, `todo_due`, `todo_completed`, `deleted_time`, `blob_updated_time` are plain millisecond numbers ("0" when zero) — never ISO; only `created_time`, `updated_time`, `user_created_time`, `user_updated_time` are ISO; floats serialize as fixed 8 decimals (4 for altitude), matching `unserialize_format`'s toFixed. Residual uncertainty: the float *text* form in real files (toFixed string vs bare number) is locked as toFixed(8/4); confirm on a real-target item at M1.
2. Exact clipper route set + query semantics the extension exercises (port from
   `packages/lib/ClipperServer.ts`).
3. Whether current Joplin ever writes `locks/` with legacy `sync_desktop_*` names (old
   targets) — accept both on read, write numeric only.
4. Dropbox listing pagination + `has_more` behavior for very large libraries (delta emulation).
5. Whether the webview ships the Start SPA build or a local Start server (M4 decision).
