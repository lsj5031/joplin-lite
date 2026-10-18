# joplin-lite

A small Go desktop client that is a **real Joplin peer** — E2EE-capable, multi-target
sync, and a Web-Clipper-compatible server — in a single cgo-free binary that starts in
under a second. It shares one library with Joplin desktop and mobile through the sync
target and reads/writes Joplin's item format losslessly.

> Work in progress. The plan lives in [INTENT.md](INTENT.md); the visual system in
> [DESIGN.md](DESIGN.md).

## UI prototype (M4a)

Three-pane workspace — notebooks rail, note list, markdown editor — under a full-width
top bar and status strip, whose signature is the sync line: a Beck-grammar route diagram
of the library's network (joplin-lite → Dropbox → Joplin desktop, with the clipper stub
and the mobile branch).

![joplin-lite workspace — desktop](docs/screenshots/workspace-desktop.png)

Below 1024px the shell collapses to single views and the status strip stacks, so the
sync line stays full width.

![joplin-lite workspace — narrow](docs/screenshots/workspace-narrow.png)

Sync states are addressable for captures (`/?state=conflict`, `/?state=locked` — the same
states as the settings popover preview). A conflict marks the target station with a
disruption cross; a lock held by another client turns the line amber and refuses the sync.

![Conflict state — disruption cross on the sync line](docs/screenshots/state-conflict.png)

![Target locked state — sync refused](docs/screenshots/state-locked.png)

The prototype runs on synthetic mock data (labeled as such in the status strip). The
footprint figures in the strip are measured: 16.4 MB for the default binary (Dropbox /
Joplin Server; rebuild with `-tags s3` for the S3 target, 24.6 MB) and 74–133 ms startup
(windows/amd64, 2026-10-17, `go build -trimpath -ldflags="-s -w"`) — not placeholders.

## Build & test

Go 1.26+, cgo-free (modernc SQLite):

```sh
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
```

Web UI (TanStack Start + React + TS, built as a **static SPA**):

```sh
cd web
npm install
npm run dev      # dev server on :3000
npm run build    # static artifact in web/dist/client (index.html + assets)
```

`npm run build` emits a directory of static files with an `index.html` fallback for
client-side routes — there is no Node/Nitro server in production. During development
the UI talks to the local clipper API (`VITE_GOJOPLIN_URL`, `VITE_GOJOPLIN_TOKEN`).

### Desktop app

The MyGO shell in [`shell/`](shell/README.md) embeds that build, starts the clipper
API in the same process, and shows the UI in the system webview:

```sh
bash shell/build.sh
./tmp/gojoplin-desktop.exe -config tmp/try-config.yaml -no-sync
```

Go 1.27+ (pinned via the toolchain) and WebView2 on Windows are the only extra
requirements; the MyGO CLI is not needed. The API address is chosen at launch and
injected into the page, so there is no build-time localhost wiring. Closing the
window stops the server and closes SQLite.

For a safe end-to-end E2EE and sync rehearsal against Joplin desktop's File
system target, see [docs/local-sync-test.md](docs/local-sync-test.md). The
memory plan for replacing the WebView2 UI with MyGO native UI is in
[docs/native-ui-migration.md](docs/native-ui-migration.md).

## Status

- **M0** (headless, cgo-free, lossless baseline) — done: golden-corpus format gate
  green, raw-passthrough serialization, sync engine rehearsed against a local folder
  backend.
- **M1** (E2EE) / **M2** (Dropbox sync) / **M3** (clipper) — code complete and tested
  against fakes and local rehearsals; the real-target gates (Joplin desktop decrypts,
  Dropbox round-trip, extension clip) are still ahead.
- **M4** — the `service/` typed seam (notes, notebooks, tags, search, conflicts, settings)
  is in, and a runnable MyGO desktop shell now serves the real UI from an in-process
  API (`shell/`). Remaining for the gate: typed bindings for settings/sync control and
  the real-target loops. See [INTENT.md](INTENT.md).

## License

AGPL-3.0-or-later, same as the upstream fork
([jescarri/go-joplin](reference/go-joplin)) and Joplin itself.
