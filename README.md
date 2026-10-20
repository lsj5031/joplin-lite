<img src="web/public/android-chrome-192x192.png" alt="Joplin Lite paper-fold icon" width="72" />

# Joplin Lite

A quiet, native desktop client for your Joplin notes. Paper-inspired colors,
Markdown preview, and a compact workspace built in Go with
[MyGO](https://github.com/egoist/mygo). One app process, with no Electron,
WebView2, or Node runtime in the desktop app.

**Alpha software.** Windows is being dogfooded with an existing encrypted
Dropbox library; its owner has confirmed that official Joplin on their phone
receives Lite's uploads. macOS has a native app bundle and local test coverage.
This is an independent client, not an official Joplin release or a claim of
complete feature parity. Back up your library before trying it.

![Native workspace with Markdown preview](docs/screenshots/native-workspace.png)

## A small workspace for everyday notes

- Preview existing notes by default; switch to Write with **Cmd/Ctrl+E**.
  New notes open in Write, with autosave.
- Browse notebooks and tags, search your library, save searches, and sort notes.
- Move notes, manage tags, toggle todos, or select several notes for bulk actions.
- Follow note history with Back/Forward and preserve conflicting versions.
- Sync with Joplin's item format and existing E2EE keys; serve the Clipper/Data
  API from the same process.

![Writing a note in the native editor](docs/screenshots/native-writing.png)

The native workspace collapses into separate views in a narrow window.

![Native workspace in a narrow window](docs/screenshots/native-narrow.png)

Screenshots use synthetic notes rendered by the current native UI; they contain
no personal library content. They illustrate the app, not a live-sync proof.

## Try it locally first

Build from source with Go 1.27.1 (the desktop build pins its toolchain).
Windows build commands use Git Bash; macOS uses a regular shell. No frontend
build or MyGO CLI is required.

```sh
git clone https://github.com/lsj5031/joplin-lite.git
cd joplin-lite
bash shell/build.sh
cp config.yaml.example tmp/local.yaml
./tmp/gojoplin-desktop.exe -config tmp/local.yaml -data-dir tmp/try-vault -no-sync
```

The `.exe` filename is also used for the macOS build. This command opens a new
local vault under `tmp/try-vault` and does not contact a sync target. If the API
port is occupied, add `-addr 127.0.0.1:41984` or another unused loopback port.

**Do not remove `-no-sync` until you have configured and reviewed the target.**
The example YAML defaults to Dropbox; an omitted YAML target defaults to S3.
Neither is a safe substitute for explicitly choosing local-only startup.

Package the desktop build:

| Platform | Command | Output |
| --- | --- | --- |
| Windows amd64 | `bash shell/package-windows.sh` | `tmp/Joplin-Lite-0.1.0-windows-amd64.zip` |
| macOS | `bash shell/bundle.sh` | `tmp/Joplin Lite.app` |

The Windows package is portable, with no installer. The macOS script uses
ad-hoc signing; it does not produce a notarized distribution. Building a
package does not publish a release. See [desktop documentation](shell/README.md)
for flags, packaging details and shortcuts.

## Using an existing Joplin library

The most isolated way to start is a new Lite vault connected to your sync
target. Directly opening an official desktop profile is also supported, but
changes that database in place:

1. Close official Joplin and back up its entire profile, including resources.
2. Pass the profile directory with `-data-dir`, initially with `-no-sync`.
3. Check notes and attachments before enabling any remote writes.

Lite recognizes `database.sqlite` and the adjacent `resources` directory.
It rejects a directory containing both that file and `gojoplin.sqlite`, and
adds local history/search tables when opening an official profile. Do not run
both desktop apps against the same SQLite database at once.

The default new-vault directory is `~/.local/share/gojoplin` (also under the
Windows user profile). Configuration is loaded from an explicit `-config`,
the Go Joplin YAML configuration, or official Joplin settings. Opening a
profile is not a complete migration of its credentials or operating-system
secure storage.

## Sync and encryption

| Target | Support and current evidence |
| --- | --- |
| File system, target 2 | Local plaintext/E2EE round-trip rehearsal |
| Dropbox, target 7 | Guarded native Windows dogfood run, independent encrypted upload verification, owner-confirmed phone receipt |
| Joplin Server, target 9 | Plaintext/E2EE Docker integration rehearsal |
| S3, target 8 | Optional `-tags s3` build; Docker integration rehearsal |

These are different levels of evidence. They do not establish all conflict,
attachment, token-expiry or deletion scenarios on every platform. OneDrive
is not implemented. Dropbox and S3 configuration is currently file-based;
the native Settings dialog edits local-folder/Joplin Server settings and
retains configured Dropbox/S3 targets.

For initial Dropbox use, enable **remote deletion protection** alongside your
authentication configuration:

```yaml
sync:
  target: 7
  dropbox:
    block_deletes: true
```

The JSON equivalent is `"sync.7.blockDeletes": true`. This is opt-in: it blocks
Lite's remote note/attachment deletions and leaves them queued with an error.
Uploads, edits, downloads, incoming deletions, and sync-lock cleanup still
work. It is not a read-only mode or a backup.

Dropbox can reuse a usable official `sync.7.auth` token. New authorization
uses your own Dropbox App-folder OAuth app through the CLI's `dropbox-auth`
command; a different OAuth app does not automatically share the official
app's folder. Refreshing a token requires its original app credentials.
See [the example config](config.yaml.example),
[the authorization command](cmd/dropbox_auth.go), and
[desktop configuration](shell/README.md) before connecting a real library.

Dropbox saves its own change cursor and complete remote inventory in the
profile. After the first full listing, later passes request only changes,
including after restarting the app. Unfinished downloads remain pending;
an invalidated Dropbox cursor safely triggers a fresh full listing. Existing
official attachment files, including `.crypted` caches, avoid a download only
when their bytes match Dropbox's content hash. Missing or mismatched files
download normally. This does not import the official client's sync cursor.

Encrypted uploads require an unlocked active key. Supply the master password
through `GOJOPLIN_MASTER_PASSWORD` rather than committing it to config. Native
Settings masks passwords visually, but saving them stores them **unencrypted
in the local database**. The private secure bootstrap used on the owner's
Windows machine is not part of the portable package.

## Current limits

- The first Dropbox inventory, cursor-reset recovery, uncached downloads and
  decryption can still take time on large libraries. Later passes use Dropbox
  changes and the saved inventory. See the
  [timing review and improvement record](docs/dropbox-sync-review.md).
- The API and native workspace are covered by tests, but actual Web Clipper
  extension pairing, assistive technology, and broad real-device testing
  remain release-validation work.
- Fresh-account onboarding, credential renewal, signed distribution and
  rollback need further testing before a stable release.
- There is no automatic updater or installer. Keep a profile backup and
  retain a known-good build when updating.

## Develop and test

The backend uses modernc SQLite and builds without cgo. The native shell is a
separate Go module pointing at the backend in this repository.

```sh
CGO_ENABLED=0 go test ./...
(cd shell && CGO_ENABLED=0 go test ./...)
bash qa/local-sync-e2e.sh
```

For race checks, use `go test -race ./internal/sync` with the platform's race
detector requirements available. Native rendering captures can be reproduced
with `CAPTURE_DIR=../tmp/ui-audit go test -run TestCaptureScreens ./` from
`shell/`.

The optional React browser UI remains separate from the native app:

```sh
cd web
npm ci
npm run build
cd ..
go build -o tmp/gojoplin .
./tmp/gojoplin serve --config tmp/local.yaml --no-sync --web-dir web/dist/client
```

The browser preview listens on `http://127.0.0.1:41185` by default; the API
uses the configured port. See [local sync](docs/local-sync-test.md),
[E2EE rehearsal](docs/e2ee-sync-rehearsal.md),
[Joplin Server](docs/joplin-server-test.md), and [S3](docs/s3-test.md) for
reproducible integration procedures. Historical footprint measurements are
in [native UI migration](docs/native-ui-migration.md); they are not a matched
benchmark against the current official Electron release.

## License

AGPL-3.0-or-later. Forked from
[jescarri/go-joplin](https://github.com/jescarri/go-joplin).
See [LICENSE](LICENSE) and [NOTICE](NOTICE). The app bundles third-party fonts
with their license files; retain those notices when redistributing.
