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
window class (title bar and taskbar). `bash shell/winres.sh` (go-winres
v0.3.3, `VERSION` defaults to `0.1.0`) rebuilds it in two steps:
`go run ./tools/icongen` writes `web/public/app-icon.ico` with a 16, 32, 48
and 256 pixel entry from the 512 px PNG — one image per size Windows asks for,
rather than letting Windows scale a single 256 px image — and go-winres packs
that icon plus the version block into the `.syso`. Commit both files after
changing the artwork or the version. There is no manifest on purpose: the app
sets per-monitor-v2 DPI awareness itself, and a manifest declaring a DPI mode
would override that and blur the window on a scaled display.

The zip is the intended Windows package: the app is portable by decision, with
no installer, no registry entries and no uninstaller. Nothing is written next
to the executable — the vault lives in `%USERPROFILE%\.local\share\gojoplin`
and the settings come from `%USERPROFILE%\.config\joplin-desktop\settings.json`
or `-config`.

## Run

```sh
./tmp/gojoplin-desktop.exe -config tmp/try-config.yaml -no-sync
```

The app logs to `<data dir>/desktop.log` (the data dir is
`%USERPROFILE%\.local\share\gojoplin` by default) as well as to stderr, and
rotates it to `desktop.log.1` at the next launch once it passes 1 MiB. That
file is the only record of a startup failure or a failing sync pass in a
packaged build, which has no console to print to; `showFatal` still shows the
same reason in a native dialog.

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
Cmd/Ctrl+E edit/preview, and back/forward: Alt+Left/Right, or Cmd+[ / Cmd+]
on macOS, where the focused editor keeps Alt and an arrow for word movement.
Edits are flushed
when switching notes, leaving the editor and closing the window.

Measured as one process at **92–101 MB** working set on windows/amd64
depending on window visibility and GC timing (94,668 KB on the final sample)
with no `msedgewebview2.exe` child, versus 7 processes and ~453 MB for the
former webview shell. The same build on macOS 27/arm64 (17.50 MiB binary) is
one process at **120 MiB RSS with a 5-note vault and 130 MiB with 1000 notes**
(~60 MB of the platform's dirty-page footprint), of which a bare MyGO window
with no data already costs 101 MiB and the headless backend alone 21 MiB —
see the macOS re-measurement in `docs/native-ui-migration.md`. `go test ./...` in this directory includes headless
`ui.NewTester` tests that type, autosave, preview, search, move, delete and
save settings through the real view.

`CAPTURE_DIR=../tmp/ui-audit go test -run TestCaptureScreens ./` renders every
screen — panes, filters, menus, dialogs, the narrow drawer and the five sync
states — through the same view and writes 25 PNGs for design review against
`DESIGN.md`; it is skipped without `CAPTURE_DIR`.

`CAPTURE_DIR=../tmp/ui-audit go test -run TestCaptureDemo ./` drives the same
view through a short product tour — preview, search, writing a note, the tag
menu, settings, the narrow drawer and the footer's sync states — and writes a
PNG per frame at 30 fps (about 26 s). Cut the clip from them with:

```sh
ffmpeg -y -framerate 30 -i ../tmp/ui-audit/demo/%04d.png \
  -c:v libx264 -pix_fmt yuv420p -crf 20 -movflags +faststart \
  ../qa/video/native-workspace.mp4
```

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

- An updated macOS bundle smoke run; Windows is packaged and portable by
  decision (no installer).
- Lock and offline states were exercised headlessly and against a fake
  engine, not by pulling a real network cable or contending with a second
  device on a live target.
- Real-target gates: clipper pairing has a native Accept/Reject dialog, but it
  has not been exercised with the actual browser extension yet; Dropbox/S3
  sync is rehearsal-tested only.
