# Desktop shell (M4)

A native window (MyGO + the system webview) around the `web/` UI, with the
clipper/Data API running **in the same process**. There is no separate
`gojoplin serve`, no Node runtime, and no localhost port contract to wire up:
the shell binds the configured address before opening the window and injects
the base URL and token into the page.

## Build

Requirements: Go 1.27+ (the module pins `go1.27.1`; `GOTOOLCHAIN=auto` fetches
it), WebView2 on Windows 10 (Windows 11 ships it), and the usual web toolchain
for the frontend.

```sh
bash shell/build.sh              # builds web/dist/client, stages it, builds the app
```

The script copies the static SPA into `shell/webdist/` (gitignored) because
`go:embed` cannot reach outside the module, then runs:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../tmp/gojoplin-desktop.exe .
```

### macOS app bundle

```sh
bash shell/bundle.sh    # -> tmp/Joplin Lite.app
```

This wraps the same binary with an `Info.plist` (`com.joplinlite.desktop`) and
`app-icon.icns`, so macOS gives the process a real app identity and Dock tile.
Packaging does not replace the runtime icon path: the shell still reads
`android-chrome-512x512.png` from the embedded bundle and applies it with
`Dock.SetIcon`; a failure only logs. `-smoke` reports the outcome as
`"icon": "ok"` (or the error) in `tmp/desktop-smoke.json`.

The staged bundle is required: a fresh checkout has only
`webdist/.gitkeep`, so `go build` alone produces an app that reports a
missing `index.html` instead of showing a blank window.

The MyGO CLI (`mygo dev`, `mygo build`) is deliberately not required. The
shell calls `mygo.App`/`mygo.NewWindow` directly and registers an `app://`
protocol handler; `bundle.sh` produces the macOS wrapper, and a later pass can
adopt `mygo build` without changing the app code.

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
| `-native` | Phase 0 spike: draw the read-only three-pane workspace with MyGO `ui` and no webview (see `docs/native-ui-migration.md`) |
| `-smoke` | development check: load, probe the live page, POST/DELETE a probe note, screenshot, quit |

`-no-sync` is needed for a config without a sync target: the YAML loader
defaults an omitted `sync.target` to S3 (target 8) and validation then asks
for a bucket. With a configured target the shell runs the same initial sync
+ 10-minute/triggered loop as `serve`.

The API token comes from the config. Without one the shell mints an
ephemeral token for the page only; it is never persisted or logged. Closing
the window shuts the HTTP server down and closes SQLite (`backend.Close` is
idempotent).

## Page/API boundary

`frontend.go` serves `webdist` with SPA fallback and injects
`window.__GOJOPLIN__ = {base, token, shell}` before `</head>`, so
`web/src/lib/api.ts` prefers runtime config over `VITE_*` build env. The
page origin is `app://localhost` (Windows: `http://app.localhost`) and the
API is `http://127.0.0.1:<port>`; the clipper's CORS middleware permits the
resulting preflights. Hashed assets are served with an immutable cache
header, `index.html` with `no-cache`.

The shell passes an all-writable clipper policy, because the desktop UI is
the owner of the database; the configured MCP allow-list still applies to
`gojoplin serve`, not to this window.

## What is still missing from the M4 gate

- Typed MyGO bindings (`service/` → `src/mygo.ts`) for settings, sync target,
  master password, and clipper token; today those stay CLI/config-file work
  and the page talks to the Data API over HTTP.
- A native "sync now"/status surface wired to the engine (the button in the
  top bar is still a prototype action), the conflict-notes view, and settings.
- Real-target gates: clipper pairing now has a native Accept/Reject dialog,
  but it has not been exercised with the actual browser extension yet;
  Dropbox/S3 sync is rehearsal-tested only.
