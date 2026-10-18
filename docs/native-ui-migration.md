# Native UI migration: the memory plan

## The measurement

On Windows, the current app is one Go process plus six WebView2 processes:

| Process | Working set |
|---|---:|
| `gojoplin-desktop.exe` | ~44 MB |
| six `msedgewebview2.exe` processes | ~409 MB |
| **total** | **~453 MB** |

The Go backend is small. The cost is Chromium's multi-process renderer, GPU,
network and utility processes. Trimming the page will not remove that floor:
even a mostly empty WebView2 window starts several of those processes.

The 16 MB binary and the ~450 MB resident UI are different numbers. Startup
and disk footprint are already good; the UI is the remaining footprint.

## Options

### A. Quick WebView2 tuning (keep the web UI)

Try `--disable-gpu`, renderer-process limits, or smaller pages. Expected gain
is modest and comes with slower rendering or unsupported behavior. This does
not remove the Chromium baseline, so it is at best a stopgap.

### B. Native chrome, WebView2 editor (hybrid)

Draw the sidebar, list, menus and status natively and keep a WebView only for
the editor/preview. Saves some renderer work but keeps the WebView process
tree alive. Rejected as the primary plan; it still pays the expensive floor.

### C. Full native UI with MyGO `ui` (recommended)

MyGO can draw the window itself on Direct3D 11, Metal or OpenGL. There is no
HTML, no JavaScript, no frontend build and no webview process. The docs'
retained-memory measurement for 1,000 labels is about 2.5 MB of Go heap, and
steady frames are hundreds of microseconds; this is a different order of
magnitude from the WebView2 tree.

MyGO already has the controls this app needs:

| Workspace piece | MyGO UI |
|---|---|
| Notebooks rail | `ui.Sidebar` / `ui.Tree` |
| Note list | `ui.List` with rows, selection and scrolling |
| Three-pane shell | `ui.SplitView` |
| Markdown preview | `ui/markdown` document view |
| Write mode | `ui.CodeEditor` or multiline `ui.TextInput` |
| Search | `ui.SearchField` |
| Note settings | `ui.Dialog`, `ui.ComboBox`, token/tag fields |
| Sync status | `ui.Badge`, `ui.ProgressBar`, `ui.Toast` |
| Conflict view | `ui.Sidebar` section + `ui.AlertDialog` |
| Links | `ui.Link` (opens the system browser) |
| Settings | native form controls |

The Go backend does not change: `service/`, `internal/store`, sync and the
clipper server stay. The native window can call `service/` directly instead
of going through the in-process HTTP API; the clipper server remains on
41184–41194 for the browser extension.

## Phases and gates

### Phase 0 — measurement spike

- A native window with a read-only three-pane shell over the existing
  snapshot: notebooks, 50-row note list, rendered Markdown for the selected
  note.
- Measure startup and total working set on Windows.
- **Gate:** under 100 MB total and visibly faster than the webview shell. If
  the spike misses this, stop and revisit.

### Phase 1 — read/write workspace

- Note selection, Markdown preview, Write mode with autosave.
- Create, rename, move, delete, todo toggle, search.
- Virtualized list (only visible rows built), keyboard navigation, back/forward.
- **Gate:** the daily note loop works without the web UI.

Status: **done.** Selection, editing, autosave, preview, create, rename,
move, delete, todo toggle, search, keyboard navigation and back/forward are
wired, and `ui.List` builds only the rows in view. The daily note loop works
without the web UI.

### Phase 2 — management and trust surfaces

- Notebook/tag management, multi-select, drag-and-drop moves, context menus.
- Sync target, master password, clipper token and conflicts in native dialogs.
- Sync-now and status; lock/conflict/error states.
- **Gate:** settings and sync no longer require editing YAML.

Status: **gate met.** The native sidebar has a sync footer with **Sync now**,
last-sync time, running/pending state and shortened errors, and the workspace
refreshes rows/notebooks (and the open note when it has no unsaved edits) when
a pass finishes. Notebooks are a title-sorted tree with create, rename, move
and delete (delete refuses a notebook with children and removes its notes
after confirmation). Tags have their own rail for filtering, and the note
header assigns/removes them; the rail can create, rename and delete tags. A
**Conflicts** row lists Joplin's virtual conflict notebook, and **Resolve
conflict → Keep this version** copies the conflict onto its original and
deletes the copy. **Settings** now edits the sync target (off / file system /
Joplin Server), its fields, the E2EE master password and the clipper token in
a native dialog; saving applies them without a restart. The note list has a
**Sort** menu (updated, created or title; either direction) with keyset
paging per order; tests pin all six orders, page boundaries, a cursor reused
with another order (ignored), legacy cursors and titles containing `:`. The
search row has **Saved**: name the active query, apply it with one click or
delete it, stored as JSON in `key_values` (`desktop.saved_searches`) so the
list is per-device and survives a relaunch (tested through a fresh app
instance; saving an existing name updates its query). Still not native:
drag/drop, multi-select and a dedicated lock-state view.

### Phase 3 — packaging

- Remove the embedded SPA from the desktop binary; keep the web build as an
  optional browser preview.
- Installer, icon, AGPL notices; record the new binary and memory numbers.
- **Gate:** no WebView2 process appears at runtime.

Status: **gate met; packaging is partly done.** `shell/main.go` has no
`go:embed` and no webview path, `shell/build.sh` no longer runs the frontend
build, and the SPA handler moved to `internal/webui`, where `gojoplin serve
--web-dir` serves it from disk on its own loopback port. The macOS bundle
carries `LICENSE`/`NOTICE` and the icon; the Windows icon/installer is still
open.

## Note-management UX to fix in the same pass

The current UI works but file-manager actions are hidden behind "Manage
note". The native rewrite is the moment to make them obvious:

- right-click context menus on notes, notebooks and tags;
- drag a note between notebooks, drag to reorder;
- inline rename (double-click), multi-select with Shift/Ctrl;
- visible new-note/new-notebook actions in each pane;
- keyboard shortcuts for new, rename, move, delete, search, sync;
- a tag picker with search instead of a flat checkbox list;
- sort menu and saved searches;
- an explicit "sync now" control.

## Risks

- Markdown rendering parity with `markdown-it` (tables, task lists, linkify).
- Text-editing details: IME, undo, selection, spellcheck.
- Accessibility and screen-reader behavior must be checked, not assumed.
- MyGO is young; pin the version and keep `service/` as the boundary.
- The visual system (Overpass, paper/navy, the sync line) must be rebuilt in
  native drawing code; screenshots and `DESIGN.md` remain the reference.

## Phase 0 result (2026-10-18)

`shell/native.go` is the spike: a read-only three-pane shell (notebooks rail,
500-row note list, rendered Markdown for the selected note) over the existing
snapshot, opened with `-native`. It reads through `service/` directly, so the
HTTP API and the webview are not involved.

| Shell | Processes | Working set |
|---|---:|---:|
| WebView2 shell | 7 | ~453 MB |
| Native spike | 1 | **~99.6 MB** |

The difference is ~354 MB, a 4.5× reduction, with no `msedgewebview2.exe`
process at all. A captured frame (`tmp/native-smoke.png`, 2534×1529 at the
window's 2× scale) confirms the content actually renders.

The 100 MB gate passed, but narrowly. Phase 1 paged the note list as
planned; the editor it added kept the total at the line instead of lowering
it (see the Phase 1 result below).

## Phase 1 result (2026-10-19)

`shell/native.go` is now a read/write workspace, not just a spike:

- notebook rail, a keyset-paged note list (100 rows, then **Load more**), and
  search over the FTS index;
- the selected note opens in a monospace Markdown editor with a title field;
  edits autosave after a 500 ms debounce (flushed on selection change,
  preview, and window close);
- **Preview** renders the same body with `ui/markdown`, **Todo** toggles
  `is_todo`, **Move** opens a notebook menu, **Delete…** asks for
  confirmation, and **+**/Ctrl+N creates a note in the current notebook
  (creating a default notebook when the vault has none);
- Back/Forward walk the visited notes; a target hidden by the current search
  or notebook drops those filters to show it;
- shortcuts: Cmd/Ctrl+N new, Cmd/Ctrl+F search, Cmd/Ctrl+S save now,
  Cmd/Ctrl+E preview, Alt+Left/Right back and forward.

It still talks to `service/` directly, so the clipper server and the webview
shell are untouched. Measured on the same snapshot, while no webview process
was running:

| Build | Processes | Working set |
|---|---:|---:|
| WebView2 shell | 7 | ~453 MB |
| Native workspace | 1 | **92–97 MB** |

After the notebook tree and tag rail landed, a windowed smoke run settled at
92 MB (97 MB in the first second), so the 100 MB gate now has a small margin.
`ui.List` builds only the rows in view; the remaining cost is text shaping and
font caches. Keeping button glyphs ASCII (for example `<` rather than `‹`)
avoids pulling in a fallback font face for a few megabytes of cache. Trimming
the editor's shaping caches is the next memory work, not list virtualization.

Verification: `go test ./...` and `cd shell && go test ./...` pass.
`TestNativeWorkspaceDrivesTheView` drives the real view headlessly through
`ui.NewTester`: it types into the body and title, waits for the autosave
debounce, toggles preview, creates a note with Ctrl+N, searches the store,
toggles Todo and deletes through the confirmation dialog.
`TestNativeHistoryAndMove` clicks between notes, walks Back/Forward by button
and Alt+Left/Right, and moves a note out of the open notebook through the
Move menu. `TestNativeNotebookManagement` creates a child through the modal,
renames it, verifies deleting it removes its notes, and checks that a parent
with children is refused; `TestNativeTagFilterAndToggle` assigns a tag, filters
the list by it, and removes it again; `TestNativeTagManagement` creates,
renames and deletes a tag through the rail; `TestNativeConflictsResolve`
opens the Conflicts view and folds a conflict back into its original. `TestNativeSyncRefresh` checks that a pulled change updates an
idle open note but never overwrites unsaved typing, and
`TestSyncStatusTransitions` covers the running/pending/error snapshot the
footer reads. `TestDesktopBackendManualSync` runs the real engine against a
file-system target, waits for the initial pass, pushes a note through
`SyncNow`, and verifies the target received it. A windowed smoke run captures
`tmp/native-smoke.png` (2534×1529).

## Phase 2 result (2026-10-19)

The management and trust surfaces are native:

- notebooks create/rename/delete/move; tags create/rename/delete, filter the
  list, and are assigned per note; notes get the **Conflicts** rail and
  **Resolve conflict → Keep this version**;
- sync is a footer line (last pass, running, pending, shortened error) with
  **Sync now**, and a finished pass refreshes rows, notebooks, tags and an
  idle open note without overwriting unsaved typing;
- **Settings** is a native dialog. It edits the sync target — off, file
  system (folder) or Joplin Server (URL, email, password) — plus the E2EE
  master password and the clipper API token (with **Regenerate**). Saving
  validates first, stores the values in the vault's local `key_values` table,
  swaps the clipper token in place (the old one is rejected immediately) and
  replaces the sync loop once its current pass has finished. The config file
  is never rewritten, so a Joplin `settings.json` profile stays owned by
  Joplin desktop. A Dropbox/S3 target configured in the file is shown as a
  read-only segment and kept.

Remaining Phase 2 UX (not the gate): drag/drop moves, multi-select, and a
first-class lock/offline state beyond the sync footer's error line.

## Phase 3 result (2026-10-19)

The desktop binary is native-only. `-native` and the embedded SPA are gone:
`main.go` opens the workspace directly, `shell/build.sh` is a single
`go build`, and `shell/webdist` no longer exists. The SPA handler now lives in
`internal/webui` and is mounted only by `gojoplin serve --web-dir <dist>`
(default `127.0.0.1:41185`, injected `base` pointing at the API port), which
keeps the web build usable in a browser without putting Chromium back in the
desktop process.

| Measurement | Phase 1 shell | Phase 3 shell |
|---|---:|---:|
| Processes | 1 | 1 |
| WebView2 processes | 0 | **0** |
| Working set | 92–97 MB | **92–101 MB** by window state (94.7 MB final sample) |
| Binary | 19.5 MiB | **18.6 MiB** (19,501,056 bytes) |

The binary shrank by the staged bundle (~0.9 MiB) and contains no
`__GOJOPLIN__` runtime-config string. A windowed smoke run captured a valid
2534×1529 frame with clean logs; `wmic` shows no `msedgewebview2.exe` whose
parent is the app.

The preview was verified against the real build: `/` injects
`{"base":"http://127.0.0.1:41186",...,"shell":"browser"}`, a client route
falls back to `index.html` (200), a missing `.js` asset 404s, and the API on
the configured port answers `/ping`.

Still open for full packaging: a Windows icon resource and installer (the
macOS bundle already ships the icon, `LICENSE` and `NOTICE`), and a signed
bundle run on macOS.

## Decision

Phases 1–3 meet their gates: the daily note loop works without the web UI,
settings and sync no longer require editing YAML, and the desktop binary runs
with zero WebView2 processes. The native workspace is the product shell; the
web build remains an optional browser preview. Remaining work is UX polish
(drag/drop, multi-select), release packaging for Windows, and the real-target
loops (extension pairing, Dropbox/S3, Joplin Server).
