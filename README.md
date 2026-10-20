<img src="web/public/android-chrome-192x192.png" alt="Joplin Lite folded-ribbon J" width="72" />

# Joplin Lite

A quiet place for your Joplin notes. A native desktop app with warm paper colors,
a simple Markdown editor, and light and dark appearances.

**[Download the alpha](https://github.com/lsj5031/joplin-lite/releases/tag/v0.1.0-alpha.2)** ·
**[Watch the demo](https://github.com/lsj5031/joplin-lite/releases/download/v0.1.0-alpha.2/Joplin-Lite-workspace-demo.mp4)** ·
[Desktop guide](shell/README.md)

![Reading a note in Joplin Lite](docs/screenshots/native-workspace.png)

Joplin Lite is an independent client, built in Go with
[MyGO](https://github.com/egoist/mygo). It runs as one native app process without
Electron or an embedded browser. This is **alpha software**, being used daily
with a real encrypted Dropbox library on macOS and Windows. Back up your notes
before connecting an existing library. It does not yet cover every official
Joplin feature.

## Get started

Download and unzip the package for your computer from the
[release page](https://github.com/lsj5031/joplin-lite/releases/tag/v0.1.0-alpha.2).

| Your computer | Package | Open |
| --- | --- | --- |
| Mac with Apple silicon | `macos-arm64.zip` | Drag **Joplin Lite.app** to Applications |
| Windows 64-bit | `windows-amd64.zip` | Extract the whole folder, then run **gojoplin-desktop.exe** |

The Mac app is ad-hoc signed, not notarized; Windows is not publisher-signed.
Your operating system may ask you to approve opening it. There is no automatic
updater. To update, quit Lite and replace its app or executable; your notes live
separately from the downloaded package.

### Try it without sync

For a first look, start with a separate local notebook collection. These commands
use a fresh Lite profile and do not contact a sync service.

**Mac — Terminal:**

```sh
"/Applications/Joplin Lite.app/Contents/MacOS/Joplin Lite" -no-sync -data-dir "$HOME/Library/Application Support/joplin-lite/try-notes"
```

**Windows — PowerShell, from the extracted folder:**

```powershell
.\gojoplin-desktop.exe -no-sync -data-dir "$env:LOCALAPPDATA\JoplinLite\try-notes"
```

Keep these flags when reopening the trial collection. If the local API port is
already in use, add `-addr 127.0.0.1:41984` or another unused port.

## Make room for your notes

- **Read first.** Existing notes open in Preview. Choose Edit to write; new notes
  are ready to type into, and changes save automatically.
- **Find things quickly.** Browse notebooks and tags, search, save a search, or
  sort your notes by title or date.
- **Keep things together.** Move notes, manage tags, use todos, or select several
  notes to move or delete them together.
- **Add attachments.** Attach a file, drag one into the editor, or paste an image.
- **Compare conflicts.** Read both complete versions before deciding which to keep.
- **Settle in.** Choose Light or Dark in Settings. The workspace adapts to narrower
  windows, and Back/Forward returns to notes you visited.

![Writing with Markdown and autosave](docs/screenshots/native-writing.png)

| Action | Mac | Windows |
| --- | --- | --- |
| New note | Cmd+N | Ctrl+N |
| Search | Cmd+K | Ctrl+K |
| Write / Preview | Cmd+E | Ctrl+E |
| Save now | Cmd+S | Ctrl+S |

[Watch the writing demo](https://github.com/lsj5031/joplin-lite/releases/download/v0.1.0-alpha.2/Joplin-Lite-writing-demo.mp4).

![Dark appearance](docs/screenshots/native-dark.png)

![A narrower workspace](docs/screenshots/native-narrow.png)

Screenshots and demo videos use synthetic notes rendered through the current
native UI. They contain no personal library data and do not simulate a real sync.

## Bring your existing Joplin library

Lite can open an official desktop profile directly, including its notes and
adjacent attachment files. **This edits the original database.**

1. Close official Joplin and back up its entire profile, including `resources`.
2. Start Lite with `-data-dir` pointing to that profile and `-no-sync` initially.
3. Check your notes and attachments, then configure sync when you are ready.

Do not open the same profile in both desktop apps at once. Lite adds its own
local history and saved-search tables. Opening a profile alone does not transfer
all credentials or secure-storage access.

On macOS, a remembered launch file can reopen the same profile from Finder and
reuse supported official Dropbox credentials and the encryption password through
Keychain. This is an explicit setup step, documented in
[remembered startup and Keychain reuse](shell/README.md#remembered-native-startup).
Recovered secrets stay in memory. Outside that mode, passwords saved in Lite's
Settings are stored unencrypted in its local database.

## Sync when you are ready

| Service | Setup | Current coverage |
| --- | --- | --- |
| Dropbox | Configuration file / existing supported credentials | Real encrypted-library use on Mac and Windows; uploads received by official Joplin on a phone |
| Joplin Server | Settings | Plaintext and encrypted integration tests |
| Local folder | Settings | Plaintext and encrypted round-trip tests |
| S3 | Configuration file; build with `-tags s3` | Integration tests; excluded from standard desktop packages |

OneDrive is not implemented. New Dropbox authorization uses your own OAuth app;
that app may have a different folder from official Joplin's app. Review the
[configuration example](config.yaml.example) and [desktop guide](shell/README.md)
before connecting an existing target.

Normal sync sends edits, uploads and deletions to your configured target. Optional
Dropbox deletion protection (`sync.dropbox.block_deletes: true` in YAML) blocks
outgoing deletions and leaves them queued with an error. It is not read-only mode
or a backup. Turn it off only when you intend local deletions to reach Dropbox.

Dropbox remembers its change cursor across restarts. Later checks request changes
rather than listing the entire library again. Existing metadata and attachments
are reused when their exact bytes match Dropbox's content hash. A first sync,
cursor reset, or encrypted metadata without a matching local copy can still need
downloads. The footer shows the current stage and last successful sync time.

## A few things to know

- Keep backups during the alpha. Fresh-account setup, credential renewal,
  accessibility, and broad device compatibility need more testing.
- The app includes a Clipper/Data API, but actual browser-extension pairing
  remains a validation gap.
- Large libraries can take time on their first sync. See the
  [sync performance notes](docs/dropbox-sync-review.md) for measured improvements
  and remaining limits.
- Logs are in `desktop.log` inside the active profile directory. Include the
  error and app version when [reporting a problem](https://github.com/lsj5031/joplin-lite/issues).
  Remove private note content, paths and credentials before sharing logs.

## Build and contribute

Go 1.27.1 is pinned by the desktop build. No frontend build is needed for the
native app. Windows scripts use Git Bash.

```sh
git clone https://github.com/lsj5031/joplin-lite.git
cd joplin-lite
bash shell/build.sh
go test ./...
(cd shell && go test ./...)
```

See the [desktop guide](shell/README.md) for packaging, reproducible screenshots
and video capture, and the optional browser preview. Integration guides cover
[local sync](docs/local-sync-test.md), [encryption](docs/e2ee-sync-rehearsal.md),
[Joplin Server](docs/joplin-server-test.md), and [S3](docs/s3-test.md).

## License

AGPL-3.0-or-later. Forked from
[jescarri/go-joplin](https://github.com/jescarri/go-joplin).
See [LICENSE](LICENSE) and [NOTICE](NOTICE). Retain bundled font licenses when
redistributing.
