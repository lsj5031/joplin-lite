<img src="web/public/android-chrome-192x192.png" alt="Joplin Lite folded-ribbon J" width="72" />

# Joplin Lite

**A quiet, native desktop home for your Joplin notes.**

Built in Go with [MyGO](https://github.com/egoist/mygo), Joplin Lite runs as a single lightweight native app (~120 MB RAM, no Electron or embedded browser) with warm Field Notes paper tones, Liquid Glass surfaces, and a fast, distraction-free Markdown editor.

**[Download v0.1.0-alpha.3](https://github.com/lsj5031/joplin-lite/releases/tag/v0.1.0-alpha.3)** ·
**[Watch the 29s workspace tour](https://github.com/lsj5031/joplin-lite/releases/download/v0.1.0-alpha.3/Joplin-Lite-workspace-demo.mp4)** ·
**[Watch the writing demo](https://github.com/lsj5031/joplin-lite/releases/download/v0.1.0-alpha.3/Joplin-Lite-writing-demo.mp4)** ·
**[Technical & desktop guide](shell/README.md)**

![Reading a note in Joplin Lite](docs/screenshots/native-workspace.png)

> **Alpha note:** Joplin Lite is an independent client used daily on macOS and Windows with encrypted Dropbox libraries. Because it is still in alpha and does not cover every official Joplin feature, **always back up your notes** before connecting an existing library.

---

## Quick start

### 1. Download and open

Grab the latest package for your computer from the **[Releases page](https://github.com/lsj5031/joplin-lite/releases/tag/v0.1.0-alpha.3)**:

| Platform | Package | How to run |
| --- | --- | --- |
| **macOS** (Apple silicon) | `Joplin-Lite-0.1.0-alpha.3-macos-arm64.zip` | Unzip and drag **Joplin Lite.app** to **Applications** |
| **Windows** (64-bit, portable) | `Joplin-Lite-0.1.0-alpha.3-windows-amd64.zip` | Extract the whole folder and run **gojoplin-desktop.exe** |

* **First launch:** The macOS app is ad-hoc signed (not notarized) and the Windows executable is not publisher-signed, so your OS may ask you to confirm opening it the first time.
* **Updating:** There is no background auto-updater. To update, quit Joplin Lite and replace the app or folder—your notes and settings live separately in your user profile directory.

### 2. Try it safely in a local sandbox (no sync required)

Want to explore the interface before connecting your real notes? Launch Joplin Lite with a separate local trial folder that never touches a sync server:

**macOS (Terminal):**

```sh
"/Applications/Joplin Lite.app/Contents/MacOS/Joplin Lite" -no-sync -data-dir "$HOME/Library/Application Support/joplin-lite/try-notes"
```

**Windows (PowerShell, inside the extracted folder):**

```powershell
.\gojoplin-desktop.exe -no-sync -data-dir "$env:LOCALAPPDATA\JoplinLite\try-notes"
```

Keep those same flags whenever you want to reopen your trial notebook collection. *(If the local Clipper API port is already taken by another app, pass `-addr 127.0.0.1:41984`.)*

---

## Made for everyday reading and writing

- **Read first, write when ready.** Existing notes open in a clean, typeset Preview with interactive checklists. Press `Cmd/Ctrl+E` (or click the pencil icon) to edit in Markdown—new notes start ready to type, and your changes save automatically after 500 ms.
- **Jump anywhere from the keyboard.** Press `Cmd/Ctrl+P` (or click `⌘K` in the search bar) to open the **Command & Note Palette**, switch notes instantaneously, toggle appearances, or trigger sync.
- **Stay organized without clutter.** Keep the sidebar focused on your note titles, or open the translucent **Notebooks & Tags** rail (`Cmd/Ctrl+\`) when you want to browse folders, filter by tag, rename inline (`F2` / `Return`), or drag notes into another notebook. Select multiple notes (`Cmd/Ctrl+Click` or `Shift+Click`) to move or delete them in bulk.
- **Attach files & images effortlessly.** Click the paperclip button, drag files straight into the editor, or paste images from your clipboard.
- **Resolve sync conflicts safely.** When two devices edit the same note, Joplin Lite shows a side-by-side comparison of both complete versions so you never lose work blindly.
- **Make the space yours.** Open **Settings** (`Cmd/Ctrl+,`) to switch between **Light** and **Dark** appearances and choose from four Oklab gradient backdrops (**Sage**, **Linen**, **Mist**, and **Kero**).

![Writing with Markdown and autosave](docs/screenshots/native-writing.png)

### Keyboard shortcuts

| Action | macOS | Windows / Linux |
| --- | --- | --- |
| New note | `Cmd+N` | `Ctrl+N` |
| Search notes | `Cmd+K` or `Cmd+F` | `Ctrl+K` or `Ctrl+F` |
| Command & note palette | `Cmd+P` | `Ctrl+P` |
| Toggle Edit / Preview | `Cmd+E` | `Ctrl+E` |
| Toggle Notebooks rail | `Cmd+\` | `Ctrl+\` |
| Save immediately | `Cmd+S` | `Ctrl+S` |
| Back / Forward in history | `Cmd+[` / `Cmd+]` | `Alt+Left` / `Alt+Right` |
| Inline rename selected item | `Return` or `F2` | `F2` |
| Settings | `Cmd+,` | `Ctrl+,` |

![Dark appearance with the Sage Oklab backdrop](docs/screenshots/native-dark.png)

![Responsive layout in a narrower window](docs/screenshots/native-narrow.png)

*All screenshots and demo videos are rendered directly from the native UI using synthetic sample notes.*

---

## Connect your existing Joplin library

Joplin Lite can open an official Joplin Desktop profile directly (`database.sqlite` and its `resources/` folder) or sync from scratch into its own profile.

### Opening an existing profile in place

> **Important:** Opening a profile directly edits that SQLite database and adds local history and saved-search tables. **Never run official Joplin and Joplin Lite against the same profile folder at the same time.**

1. **Quit official Joplin** completely and make a full backup copy of your profile folder (including `resources/`).
2. **Start in local-only mode first** so you can inspect your notes and attachments before any sync runs:
   - Point `-data-dir` to your profile directory and include `-no-sync`.
3. **Enable sync when you are satisfied.** Configure your target in Settings (`Cmd/Ctrl+,`) or your config file.

On **macOS**, you can also configure a remembered Finder launch file (`~/Library/Application Support/joplin-lite/launch.json`) that reopens your chosen profile automatically and reads your existing official Dropbox token and E2EE master password directly from **macOS Keychain**—keeping recovered secrets in memory only. See **[Remembered native startup & Keychain reuse](shell/README.md#remembered-native-startup)** for setup details. *(Outside Keychain reuse mode, passwords saved in Joplin Lite's Settings dialog are stored unencrypted in its local SQLite table.)*

---

## Sync options

| Sync target | Where to configure | Status & test coverage |
| --- | --- | --- |
| **Dropbox** | Config file / existing credentials | Used daily on macOS & Windows with encrypted libraries; interoperates with official Joplin mobile & desktop |
| **Joplin Server** | In-app Settings (`Cmd/Ctrl+,`) | Plaintext and end-to-end encrypted (E2EE) integration tests |
| **Local folder** | In-app Settings (`Cmd/Ctrl+,`) | Plaintext and E2EE round-trip integration tests |
| **Amazon S3 / S3-compatible** | Config file (build with `-tags s3`) | Integration-tested; omitted from standard prebuilt downloads to keep binaries small |

*OneDrive is not currently supported. Authorizing a brand-new Dropbox connection uses your own Dropbox OAuth app key—see [`config.yaml.example`](config.yaml.example) and the [desktop guide](shell/README.md).*

### Smart, safe Dropbox sync

- **Incremental delta sync:** Joplin Lite saves its Dropbox cursor across restarts, so subsequent syncs only fetch changed items instead of scanning your whole vault.
- **Content-hash reuse:** Existing local metadata and attachments are verified against Dropbox's content hash and reused without re-downloading.
- **Clear status in the sidebar footer:** Always see whether you are **Local only**, **Checking items**, **Synced**, **Offline**, or waiting on another device's sync lock, alongside the last successful sync time.
- **Optional deletion guard:** Setting `sync.dropbox.block_deletes: true` in YAML prevents outgoing remote deletions while you test. Turn it off when you want local note deletions to sync to Dropbox.

---

## Good to know

- **Keep regular backups** while Joplin Lite is in alpha. Fresh-account onboarding, OAuth token renewal, screen-reader accessibility, and live browser-extension pairing for the built-in Clipper API still need broader testing.
- **First sync on large vaults:** An initial sync or full cursor reset on a large library can take time; see the [Dropbox sync performance notes](docs/dropbox-sync-review.md).
- **Troubleshooting & logs:** Diagnostic logs are written to `desktop.log` inside your active data directory (automatically rotated at 1 MiB). When [opening an issue](https://github.com/lsj5031/joplin-lite/issues), include the app version and relevant error lines with personal note titles, paths, and tokens removed.

---

## Build from source

Requires **Go 1.27+** (`go1.27.1` is pinned and fetched automatically via `GOTOOLCHAIN=auto`). No Node.js or frontend build step is needed for the native desktop app.

```sh
git clone https://github.com/lsj5031/joplin-lite.git
cd joplin-lite
bash shell/build.sh          # builds tmp/gojoplin-desktop.exe
bash shell/bundle.sh         # macOS: packages tmp/Joplin Lite.app
go test ./...
(cd shell && go test ./...)
```

See [`shell/README.md`](shell/README.md) for Windows portable packaging, headless UI screenshot/video generation, and the optional browser preview. Additional testing guides cover [local sync](docs/local-sync-test.md), [E2EE sync](docs/e2ee-sync-rehearsal.md), [Joplin Server](docs/joplin-server-test.md), and [S3](docs/s3-test.md).

## License

[AGPL-3.0-or-later](LICENSE). Forked from [jescarri/go-joplin](https://github.com/jescarri/go-joplin); see [NOTICE](NOTICE) and retain bundled font licenses when redistributing.
