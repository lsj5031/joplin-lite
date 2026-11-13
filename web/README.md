# Joplin Lite browser preview

This optional React/TypeScript SPA provides a browser workspace over the local
Go Clipper/Data API. The native desktop uses MyGO and calls `service/` directly;
it does not embed this build or need Node at runtime.

## Try the interface with disposable demo data

Use a current Node.js version supported by Vite 8 (Node 22.12+ or 24) and npm.
From `web/`:

```sh
npm ci
npm run dev -- --host 127.0.0.1
```

Open the local URL printed by Vite (normally `http://127.0.0.1:3000`). With no
backend URL configured, the app uses synthetic notes. Demo edits are temporary
and disappear on reload. It never silently falls back to demo data after a
configured backend fails; that state shows a connection error.

## Serve a local database

From `web/`, run `npm ci` and `npm run build`. The build checks TypeScript and
writes the static SPA to `dist/client`. From the repository root, build the CLI:

```sh
go build -o tmp/gojoplin.exe .
```

For an isolated trial on Windows, these PowerShell commands create a disposable
profile. The two credentials below are public test values for loopback testing;
use private values for a real vault. No sync remote is contacted with `--no-sync`.

```powershell
New-Item -ItemType Directory -Force tmp/browser-preview | Out-Null
Set-Content tmp/browser-preview/config.json '{"api.token":"browser-preview-disposable"}'
$env:GOJOPLIN_DATA_DIR = "$PWD/tmp/browser-preview/vault"
$env:GOJOPLIN_API_KEY = 'browser-preview-disposable-key'
$env:GOJOPLIN_PORT = '41888'
$env:GOJOPLIN_MCP_ALLOW_FOLDERS = '*'
$env:GOJOPLIN_MCP_ALLOW_TAGS = '*'
$env:GOJOPLIN_MCP_ALLOW_CREATE_FOLDER = 'true'
$env:GOJOPLIN_MCP_ALLOW_CREATE_TAG = 'true'
& ./tmp/gojoplin.exe serve --config tmp/browser-preview/config.json --no-sync --web-dir web/dist/client --web-addr 127.0.0.1:41185
```

Open `http://127.0.0.1:41185`. `serve` injects the API address, token and profile
recovery identity into the HTML; no `VITE_` variables are needed. The preview
and API use separate ports because the API owns paths such as `/notes`.
`GOJOPLIN_WEB_DIR` is an alternative to `--web-dir`. Keep both listeners on
loopback: the preview page contains the API token. Close this test terminal
when finished to discard its environment overrides. To use real notes, select
an explicit config and backed-up profile; browser edits change that database.

For frontend development against that API, set `VITE_GOJOPLIN_URL` and
`VITE_GOJOPLIN_TOKEN` before starting Vite. These values are client-visible;
never commit credentials or distribute a build containing private ones.
Backend mutation policy still applies. A read-only or rejected action reports
an error; the browser cannot grant itself access.

## Supported scope

- Paged note summaries, selected-note body loading, search, notebook/tag filters,
  note creation and Markdown editing with local autosave and retry/recovery state.
- Note title/notebook/tag management, deletion confirmation, note-level to-do
  completion, notebook/tag creation, Markdown preview and internal note links.
- Responsive list/editor navigation, notebook/tag dialogs, local save/connection
  status and Light/Dark/System appearance saved in browser storage.
- Cmd+K on Apple platforms or Ctrl+K elsewhere focuses search; Escape clears
  the query. Open dialogs keep focus and suspend the search shortcut. Note rows
  support arrow keys, Home/End and Enter/Space activation.

Reload refreshes the local database and retries failed local saves. It is not
a remote sync control. Sync targets, credentials and encryption belong to the
backend configuration. This preview has no native Ask panel, command palette,
saved searches, bulk/drag operations, JEX dialogs or equivalent conflict-resolution
workflow. Conflict copies can be viewed, but use the native app to resolve them.
Markdown task checkboxes are read-only in preview; edit their markers in Write.
Browser theme settings do not change native desktop settings.

The shared palette and typography roles are described in [DESIGN.md](../DESIGN.md).
The browser retains its own editorial sizing and continuous paper layout.

## Checks

`npm run build` runs Vite and `tsc --noEmit`. Existing DOM checks use an installed
Playwright module and Chrome executable; set `PLAYWRIGHT_MODULE`, `CHROME_PATH`
and the indicated origin before running them from the repository root:

```sh
node web/appearance.test.mjs
node web/note-list.test.mjs
node web/data-layer.test.mjs
```

`appearance.test.mjs` can run against the synthetic demo (`UI_TEST_ORIGIN`).
The note-list and data-layer checks require a disposable writable API and use
`NOTE_LIST_*` / `DATA_TEST_*` variables documented at the top of each script.
The note-list fixture needs more than 50 notes. Never point mutation checks at
a personal profile. `npm run preview` serves a build for inspection but does not
inject a backend configuration; use `serve --web-dir` for that runtime contract.
