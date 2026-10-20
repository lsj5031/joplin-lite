# Surface brief — joplin-lite native workspace (`shell/`)

Scope: the primary desktop application workspace (`shell/workspace.go`, `shell/design.go`,
`shell/native.go`, `shell/settings.go`). Mode: Operate. Build path: code-led native Go UI
rendered with MyGO `ui`.

Audience: the project owner — a developer who lives in one Joplin library shared with
desktop/mobile clients over Dropbox with E2EE. Job: find, read, edit, and clip notes fast,
and know at a glance whether the library is synced and the vault is unlocked. Frequency:
many times daily, keyboard-first, long sessions in even light. Task states that must read
clearly: populated library, Markdown preview and writing mode, search hit / search empty,
conflict note present and resolvable, bulk selection, syncing with live progress, sync
error/target locked, and narrow single-column window with notebook drawer.

Chosen direction: **Field Notes — Humanist** (`DESIGN.md`).

## Direction contract

THESIS: A quiet place to write: a two-pane desktop workspace with a 300px sage binding column
and a warm paper editor/reader capped at a 700px reading measure, opening notebooks and tags on
demand rather than keeping three permanent chrome columns on screen.

OWN-WORLD: Warm paper `#F9FAF5`; sage binding `#E9EEE3`; green-charcoal ink `#344238`;
secondary sage ink `#606F5A`; forest accent `#566C4A`; hairlines `#DCE3D6` / `#A8B69F`;
functional semantic signals (`#2E7D4F` synced, `#C9821B` warning/conflict count, `#C8322B`
destructive/error). Optima for wordmark and note titles, Avenir Next for reading and controls,
Overpass Mono for timestamps, counts, and code.

STORY: Existing notes open in calm Markdown preview; new notes and `Cmd/Ctrl+E` open writing
mode with the caret at the top of the note and 500ms debounced autosave. Notebooks and tags
open inline on desktop (preserving drag-and-drop onto notebooks) or as a single-header drawer
in narrow windows. The footer spells out sync state in plain text with live phase progress.

FIRST VIEWPORT: Two-pane desktop layout — 300px sage column (folded-sheet mark + wordmark,
search input, `+ New note` and `Notebooks` triggers, list header with `Back`/`Forward` and
`Sort`, `Saved searches`, and hairline-ruled note rows with title + date on the top line and
full-width excerpt below) paired with the warm paper note surface (Optima title, mono metadata,
`Tags`/`Move`/`Delete…` actions, `Todo` and `Edit`/`Preview` toggle, and 700px body column)
above a single-line status footer.

