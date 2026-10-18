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

### Phase 2 — management and trust surfaces

- Notebook/tag management, multi-select, drag-and-drop moves, context menus.
- Sync target, master password, clipper token and conflicts in native dialogs.
- Sync-now and status; lock/conflict/error states.
- **Gate:** settings and sync no longer require editing YAML.

### Phase 3 — packaging

- Remove the embedded SPA from the desktop binary; keep the web build as an
  optional browser preview.
- Installer, icon, AGPL notices; record the new binary and memory numbers.
- **Gate:** no WebView2 process appears at runtime.

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

## Decision

Do the Phase 0 spike before committing to the rewrite. It is a bounded piece
of work that answers the only question that matters: what does the native
window actually cost on this machine?
