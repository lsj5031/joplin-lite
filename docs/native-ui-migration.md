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
instance; saving an existing name updates its query). The note list uses
MyGO's keyed `Selection` for multi-select (Cmd/Ctrl-click, Shift ranges,
Cmd/Ctrl+A): a count bar moves the chosen notes to a notebook or deletes them
after confirmation, pending edits to a note being deleted are dropped rather
than written back, and the choice is pruned when a filter changes. Each row
has a `::` grip that drags it (or the whole choice) onto a notebook; the
grip, not the row, is the drag source because a draggable row would take the
press from the list and a click would stop choosing rows. Only a dedicated
lock/offline view is left from the Phase 2 list.

### Phase 3 — packaging

- Remove the embedded SPA from the desktop binary; keep the web build as an
  optional browser preview.
- Installer, icon, AGPL notices; record the new binary and memory numbers.
- **Gate:** no WebView2 process appears at runtime.

Status: **gate met; packaging is partly done.** `shell/main.go` has no
`go:embed` and no webview path, `shell/build.sh` no longer runs the frontend
build, and the SPA handler moved to `internal/webui`, where `gojoplin serve
--web-dir` serves it from disk on its own loopback port. The macOS bundle
carries `LICENSE`/`NOTICE` and the icon. The Windows executable carries the
same icon and a version block (resource `#1`, which MyGO loads for the window
class) from `shell/rsrc_windows_amd64.syso`, generated by the pinned go-winres
in `shell/winres.sh`; the build uses the GUI subsystem so no console appears.
The icon itself is written by `tools/icongen` (`internal/iconico`) from the
512 px artwork with a 16, 32, 48 and 256 pixel entry, so Windows does not have
to scale one image for the title bar and the file list. Because a GUI build has
no console, the app also logs to `<data dir>/desktop.log` (rotated at 1 MiB),
which `TestStartBackendLogsToDataDir` exercises through the real startup path
and `TestDesktopLogWritesAndRotates` covers for rotation and logger restore.
`shell/package-windows.sh` produces a portable zip with the executable,
`LICENSE`, `NOTICE` and a short readme. Windows is portable by decision:
there is no installer, no registry entry and no uninstaller.

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
  Cmd/Ctrl+E preview, and back/forward: Alt+Left/Right, or **Cmd+[ / Cmd+]**
  on macOS, where MyGO keeps Alt and an arrow for the focused editor, as every
  native text field does (word-by-word caret movement).

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
and by the platform's key (Cmd+[ / Cmd+] on macOS, Alt+Left/Right elsewhere,
plus a macOS assertion that Alt+Left in the open note stays the editor's word
movement), and moves a note out of the open notebook through the Move menu. `TestNativeNotebookManagement` creates a child through the modal,
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
- sync is a footer state with **Sync now**, and a finished pass refreshes
  rows, notebooks, tags and an idle open note without overwriting unsaved
  typing. The footer names the state instead of showing one truncated Go
  error: **Local only** (with **Set up sync** instead of a **Sync now** that
  could do nothing), **Sync paused · another device is syncing** when the
  target's lock is held, **Offline · sync paused** when the target cannot be
  reached, **Sync stopped · target needs a newer Joplin** for an unsupported
  format, and **Sync failed** plus the reason otherwise. Locked and offline
  passes are retried by the loop itself after 45 s and 30 s, and the footer
  says when ("retrying at 15:05"); a failure that needs the user waits for the
  ten-minute pass or a click;
- **Settings** is a native dialog. It edits the sync target — off, file
  system (folder) or Joplin Server (URL, email, password) — plus the E2EE
  master password and the clipper API token (with **Regenerate**). Saving
  validates first, stores the values in the vault's local `key_values` table,
  swaps the clipper token in place (the old one is rejected immediately) and
  replaces the sync loop once its current pass has finished. The config file
  is never rewritten, so a Joplin `settings.json` profile stays owned by
  Joplin desktop. A Dropbox/S3 target configured in the file is shown as a
  read-only segment and kept.

`TestSyncNoticeStates` pins the footer's words for each state and
`TestNativeFooterShowsPausedStates` renders them through a real backend's
status. `TestSyncRetryPolicy` and `TestSyncLoopRetriesTransientFailures` check
which failures the loop retries on its own — a `jsync.ErrExclusiveLock` and
connection errors (`net.Error` or a "no such host"/"connection refused"
message) — and that a rejected login is not retried behind the user's back.
The delays are package variables so the loop test runs in milliseconds.

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

Windows packaging is done and portable by decision (icon, version block,
portable zip; no installer). Still open: a signed bundle run on macOS (the
bundle already ships the icon, `LICENSE` and `NOTICE`).

## Phase 3 re-measurement on macOS (2026-10-19)

The numbers above are windows/amd64. The same code was measured on macOS
27.0.1/arm64 (Go 1.27, `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`)
with two throwaway vaults seeded through the clipper API — 5 notes, and 1000
notes (~1.5 KB bodies, 3.0 MB of SQLite) — a 1280×800 window opened with
`-smoke -smoke-after Ns`, `ps -o rss` sampled every 0.5 s and `vmmap -summary`
for the platform's own footprint. Three runs per row unless noted, and a
throwaway minimal MyGO window (one text, a button, an input and a text area,
same size, no data) as the framework floor.

| Measurement (macOS 27.0.1, arm64) | Value |
|---|---:|
| Processes (with children) | 1 (none) |
| First frame + capture + shutdown, minus the smoke delay (10 runs) | 0.116–0.141 s after the process's first log line |
| Backend listening | ≤1 ms after the process's first log line |
| Binary | 18,354,370 bytes (17.50 MiB); windows/amd64 is 18.60 MiB |
| Steady RSS, 5-note vault | 119.8–120.5 MiB |
| Steady RSS, 1000-note vault | 129.8–131.0 MiB |
| Steady RSS, headless `serve` on the same vault (no window) | 21.4 MiB |
| Steady RSS, minimal MyGO window (control) | 100.7–101.0 MiB |
| macOS physical footprint (dirty-only) | 59–133 MB across runs; it swings with GC, so RSS is the stable metric |
| Idle CPU, window open, 1000-note vault | 0.99% of one core over 11 s |
| Go heap live (`GODEBUG=gctrace=1`) | 8 MB warm, 23 MB by 25 s against a 27 MB goal; 7 collections in 25 s |

What the numbers say:

- **The window is the cost, not the workspace.** A minimal MyGO window with no
  data measures 101 MiB here; the workspace sits 19–30 MiB above it, and a
  headless `serve` of the very same vault is 21 MiB. 200× the notes adds ~10 MiB
  (120 → 130): paging the list keeps the library's cost bounded.
- **The floor is the platform stack.** The control's `vmmap` regions are the
  workspace's — ~32 MB of resident shared `__AUTH_CONST` plus ~17.5 MB of
  `__DATA_CONST` (Cocoa/CoreGraphics/Metal), a 15 MB `IOSurface` (the single
  2×-scaled 1280×800 backing store) and ~11 MB of graphics-side malloc. RSS
  counts those shared pages; the footprint metric, which counts dirty ones,
  read 59 MB in one run and 133 MB in another of the same build.
- **Startup is not the problem.** The window is up, captured and shut down
  inside 0.15 s of the process's first log line, and the backend is listening
  within 1 ms of it.
- **The <100 MB gate does not transfer to macOS.** On Windows the window
  included measured 92–101 MB; on macOS the *empty* window floor alone is
  ~101 MiB RSS. The gate therefore needs a metric and a platform: on macOS the
  honest statement is one process, ~120 MiB RSS (~60 MB footprint) with a
  1000-note vault.
- **The earlier "trim the editor's shaping caches" guess is bounded.** The
  control already contains a text area, so its shaping and font caches are
  inside that 101 MiB floor; trimming ours is worth single-digit MB at best.
  The remaining app-attributable memory is the Go heap (≤27 MB) and one
  backing surface.

Nothing here changes the Phase 3 gate as written ("no WebView2 process appears
at runtime"), which the macOS run satisfies too; it does mean the Phase 0
memory gate should be restated per platform before it is judged again.

## Design pass (2026-10-19)

The workspace draws `DESIGN.md` from one place, `shell/design.go`: the palette
(paper/navy chrome, violet selection, the service colors), the type scale,
the 245/360/1fr grid metrics, the motion budget, the `designTheme` the
widgets read, and the sync line. `shell/workspace.go` lays out the chrome and
the three panes against those tokens, so no screen picks its own color or size.

`TestCaptureScreens` renders every state through the real view into
`CAPTURE_DIR` — workspace, preview, notebook and tag filters, conflicts, search
with and without results, bulk selection, each menu and dialog, the empty
editor and empty vault, the narrow list/drawer/editor, and the five sync
states — 25 PNGs at 1280×800 and 900×640. `capture` mounts a surface, runs its
entrance out, and renders the settled frame, so a screenshot never catches a
panel at opacity 0.

`TestCaptureDemo` drives the same view through a 26-second tour — preview,
search, writing a note, the tag menu, settings, the narrow drawer, the footer's
states — and writes 783 frames at 30 fps; `qa/video/native-workspace.mp4` is
the H.264 cut of them. The tour found the one behavioral bug of the pass: a
search query that narrowed the list moved the keyboard into the note the
reload had just opened, so every character after the first was typed into that
note's body and autosaved. The search field now clears the pending body focus,
and `TestNativeSearchKeepsTheKeyboard` types a query one frame at a time to
pin the behavior.

Auditing those frames fixed what the tests could not see: the search query was
drawn in ink on navy (the field now sets its text and selection colors), chosen
list rows were a solid accent fill with unreadable text (the list is built
under `listTheme`, violet 10% with ink text, as DESIGN.md's chosen row), the
last tag row was clipped by the status strip (the tag section sizes from the
row metric), row grips showed at rest and read as stray squares (they come
with hover), deletion confirmations made **Cancel** the violet default (the
safe choice is first, the action last and in signal red), dialog primaries
were violet rather than route blue (they use `primaryAction`), and the empty
editor named its state twice.

## Decision

Phases 1–3 meet their gates: the daily note loop works without the web UI,
settings and sync no longer require editing YAML, and the desktop binary runs
with zero WebView2 processes on Windows and zero framework processes on macOS.
The native workspace is the product shell; the web build remains an optional
browser preview. Remaining work is the signed macOS bundle run and the
real-target loops (extension pairing, Dropbox — the Joplin Server and S3
targets have their own Docker round trips), plus restating the memory gate per
platform as the re-measurement above recommends.
