# Desktop app (native)

A native window drawn by MyGO `ui` over `service/`, with the clipper/Data API
running **in the same process**. There is no embedded SPA, no WebView2 process,
no Node runtime at run time, and no localhost port contract between the UI and
the API: the shell binds the configured address before opening the window and
calls `service/` directly.

## Build

Requirements: Go 1.27+ (the module pins `go1.27.1`; `GOTOOLCHAIN=auto` fetches
it). No frontend toolchain is needed for the app.

```sh
bash shell/build.sh              # -> tmp/gojoplin-desktop.exe
```

The script only runs `go build` with `-trimpath` and `-s -w`, plus
`-H=windowsgui` when the target is Windows so double-clicking the executable
does not open a console window. There is no `npm` step and nothing is staged
for `go:embed`.

### macOS app bundle

```sh
bash shell/bundle.sh    # -> tmp/Joplin Lite.app
```

This wraps the same binary with an `Info.plist` (`com.joplinlite.desktop`),
`app-icon.icns`, and `LICENSE`/`NOTICE` under `Contents/Resources/licenses/`.
The bundle is the package and supplies the icon; the Go code no longer calls
`Dock.SetIcon`.

### Windows packaging

```sh
bash shell/package-windows.sh   # -> tmp/Joplin-Lite-0.1.0-windows-amd64.zip
```

The zip holds `gojoplin-desktop.exe` (GUI subsystem: double-clicking opens no
console), `LICENSE`, `NOTICE` and a short `README.txt`. The executable's icon
and version block come from `shell/rsrc_windows_amd64.syso`, which `go build`
embeds automatically; the icon is the same 512 px artwork the web preview and
the macOS bundle use, and it is resource `#1` because MyGO loads that for the
window class (title bar and taskbar). Regenerate the resource with

```sh
bash shell/winres.sh            # go-winres v0.3.3, VERSION defaults to 0.1.0
```

after changing the artwork or the version, then commit the `.syso`. There is
no manifest on purpose: the app sets per-monitor-v2 DPI awareness itself, and
a manifest declaring a DPI mode would override that and blur the window on a
scaled display.

Still open: a real installer (Inno Setup, NSIS or MSIX); the portable zip and
the macOS bundle are the current packages.

## Run

```sh
./tmp/gojoplin-desktop.exe -config tmp/try-config.yaml -no-sync
```

| Flag | |
|---|---|
| `-config` | config file; defaults like `gojoplin serve` (Go Joplin YAML, else Joplin's `settings.json`) |
| `-data-dir` | override the database directory |
| `-no-sync` | local-only: no remote, no background sync |
| `-addr` | override the API listen address |
| `-smoke` | development check: capture `tmp/native-smoke.png` after the first frames, then quit |
| `-smoke-after` | how long to let the window render before the capture (default 6s) |

`-no-sync` is needed for a config without a sync target: the YAML loader
defaults an omitted `sync.target` to S3 (target 8) and validation then asks
for a bucket. With a configured target the shell runs the same initial sync
+ 10-minute/triggered loop as `serve`.

The API token comes from the config or from a token saved in **Settings**.
Without one the shell mints an ephemeral token for the session; it is not
persisted or logged until the dialog is saved. Closing the window shuts the
HTTP server down, waits for the last sync pass, and closes SQLite
(`backend.Close` is idempotent).

## Workspace

The window has a notebook tree with create/rename/delete, a keyset-paged note
list with search, **Saved** searches and a **Sort** menu (updated, created or
title; either direction), tags with create/rename/delete, filtering and
per-note assignment, a Conflicts view with **Resolve conflict** (keep this
version on the original), a title and Markdown body editor that autosaves
after 500 ms, a rendered preview, new note, todo toggle, move to another
notebook, delete with confirmation, and Back/Forward through visited notes.
Sorting pages with the same keyset cursor, so a deep page never scans the rows
before it. Saved searches name the active query, apply it with one click and
live in `key_values` (`desktop.saved_searches`), so they are per-device and
never synced.

The note list is multi-select (Cmd/Ctrl-click adds a row, Shift-click a range,
Cmd/Ctrl+A all): a bar above the list shows the count and moves every chosen
note to a notebook or deletes them after confirmation. The choice is pruned
whenever the rows change, so it never counts a hidden note; pending edits to a
note that is being deleted are dropped instead of written back.

Each row has a `::` grip: dragging it onto a notebook moves that note there,
or the whole selection when the dragged row is part of it. The grip, not the
row, is the drag source so a click still chooses rows.

The sidebar footer shows sync state, a **Sync now** button and **Settings**;
when a pass finishes, rows and notebooks reload and an idle open note picks up
pulled changes (unsaved edits are never overwritten). The state is spelled out
rather than folded into one error line: **Local only** with a hint that notes
stay on this computer (and **Set up sync** in place of the unusable **Sync
now**), **Sync paused · another device is syncing** when the target's lock is
held, **Offline · sync paused** when the target cannot be reached, **Sync
stopped · target needs a newer Joplin** for an unsupported format, and
**Sync failed** with the reason for everything else. A lock or a lost
connection is retried by the loop itself (45 s and 30 s), and the footer says
when ("retrying at 15:05"); failures that need the user wait for the ten-minute
pass or **Sync now**.

**Settings** edits the sync target (off, file system folder, or Joplin Server
URL/email/password), the E2EE master password and the clipper API token
(**Regenerate** mints a new one). Values are stored in the vault's local
`key_values` table — never in the config file, which may be Joplin desktop's
`settings.json` — and are applied on save: the clipper accepts only the new
token at once, and the sync loop is replaced after its current pass ends.
The master password is stored locally and unencrypted, like Joplin's own
settings; it never leaves the device. Dropbox and S3 targets still come from
the config file and are kept (shown read-only) by the dialog.

Shortcuts: Cmd/Ctrl+N new note, Cmd/Ctrl+F search, Cmd/Ctrl+S save now,
Cmd/Ctrl+E edit/preview, Alt+Left/Right back and forward. Edits are flushed
when switching notes, leaving the editor and closing the window.

Measured as one process at **92–101 MB** working set depending on window
visibility and GC timing (94,668 KB on the final sample) with no
`msedgewebview2.exe` child, versus 7 processes and ~453 MB for the former
webview shell. `go test ./...` in this directory includes headless
`ui.NewTester` tests that type, autosave, preview, search, move, delete and
save settings through the real view.

## Browser preview (optional)

The web build is not embedded in any binary. To use it in a browser over the
same vault, serve it from disk — the preview gets its own loopback port because
the Data API owns the top-level paths (`/notes`, `/tags`, …):

```sh
cd web && npm run build
cd .. && go build -o tmp/gojoplin.exe .     # the headless CLI
./tmp/gojoplin.exe serve --config tmp/try-config.yaml --no-sync \
  --web-dir web/dist/client --web-addr 127.0.0.1:41185
# open http://127.0.0.1:41185
```

`internal/webui` injects `{base, token, shell: "browser", profile}` before
`</head>`, so the page reaches the API on its configured port; hashed assets
are immutable and client routes fall back to `index.html`. `GOJOPLIN_WEB_DIR`
sets the directory without the flag.

## What is still missing

- A Windows installer (the icon and portable zip are done), and an updated
  macOS bundle smoke run.
- Lock and offline states were exercised headlessly and against a fake
  engine, not by pulling a real network cable or contending with a second
  device on a live target.
- Real-target gates: clipper pairing has a native Accept/Reject dialog, but it
  has not been exercised with the actual browser extension yet; Dropbox/S3
  sync is rehearsal-tested only.
