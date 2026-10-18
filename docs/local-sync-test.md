# Local E2EE + sync test

This is the safe way to exercise the full Joplin item format, E2EE and sync
loop without touching your production Dropbox library.

For the automated version of the same target — two throwaway vaults, a
plaintext round trip and an E2EE one, no Joplin desktop needed — run
`bash qa/local-sync-e2e.sh`. Use this document when the point is the
**Joplin desktop interop**, which that script cannot cover.

For the fast client-side gate (encrypted push, pull and decrypt without
Joplin desktop), see [e2ee-sync-rehearsal.md](e2ee-sync-rehearsal.md) first.

It uses Joplin's **File system** sync target (target 2) and a throwaway
folder plus a throwaway Joplin profile. Both clients read and write the same
on-disk Joplin format (`<id>.md`, `.resource/<id>`, `info.json`, `locks/`).

> Dropbox cannot be used for this particular test with two different OAuth
> apps. Dropbox scopes an app-folder app to its own `/Apps/<app>` directory,
> so joplin-lite's custom app and Joplin's built-in app see different folders.
> File system sync is the local interop target.

## 1. Create the test target

Create an empty folder, for example:

```text
C:\joplin-sync-test
```

Use a **new Joplin profile** (File → Switch profile → New profile), so the
test can never change your real library's sync target.

In Joplin desktop:

1. Options → Synchronization → **File system**.
2. Set the directory to `C:\joplin-sync-test`, Apply, then Synchronize.
3. Create a notebook `E2EE test` and a note `hello from Joplin`, then sync.
4. Options → Encryption → **Enable encryption**, set a master password,
   Apply, then Synchronize. Wait until the sync finishes and the target
   contains encrypted items.

Check the target before going further:

```sh
grep -E '^(encryption_applied|master_key_id):' C:/joplin-sync-test/<note-id>.md
# encryption_applied: 1
head -c 5 C:/joplin-sync-test/info.json
```

The note's body must not appear in plaintext in the `.md` file. `info.json`
must contain `e2ee` and a `masterKeys` entry.

## 2. Point joplin-lite at the same folder

Use a **new data directory**, so the test starts from an empty local database:

```sh
CGO_ENABLED=0 go build -o tmp/gojoplin.exe .
```

`tmp/e2ee-test.yaml`:

```yaml
sync:
  target: 2
  local_dir: "C:\\joplin-sync-test"

api:
  token: "e2ee-test-token"
  key: "e2ee-test-key"
  master_password: "${GOJOPLIN_MASTER_PASSWORD}"

server:
  data_dir: "C:\\joplin-lite-e2ee-test"
  port: 41185
  listen_host: "127.0.0.1"
```

Use the exact Joplin master password. It is intentionally not trimmed:

```sh
export GOJOPLIN_MASTER_PASSWORD='the exact password'
./tmp/gojoplin.exe sync --config tmp/e2ee-test.yaml
```

The log should include `loaded sync info ... e2ee=true`, `decrypting pulled
items`, and `sync completed successfully`.

Verify the pull decrypted into the local store:

```sh
sqlite3 -readonly "C:/joplin-lite-e2ee-test/gojoplin.sqlite" \
  "select title, body, encryption_applied from notes;"
```

The row must have the plaintext body and `encryption_applied = 0`.

## 3. Edit in joplin-lite, read in Joplin

1. Start the desktop app against the test config (no `-no-sync`):

   ```sh
   ./tmp/gojoplin-desktop.exe -config tmp/e2ee-test.yaml
   ```

2. Edit the test note and save it.
3. Wait for the app's sync trigger/loop, or close the app and run the CLI
   sync again:

   ```sh
   ./tmp/gojoplin.exe sync --config tmp/e2ee-test.yaml
   ```

4. In Joplin desktop, Synchronize. The edit must appear, and the target file
   must still be an encrypted envelope (`encryption_applied: 1`, no plaintext).
5. Edit in Joplin, Synchronize, then sync joplin-lite again (close the app
   first if it holds the database), reopen the app, and confirm the change.

## 4. Conflict test

On the same note:

1. Edit and sync from joplin-lite.
2. Before syncing joplin-lite again, edit the note in Joplin and sync.
3. Sync joplin-lite.

The local version must become a Joplin conflict note (`is_conflict = 1`),
with the remote version kept as the canonical note. Nothing should be
silently overwritten.

## Troubleshooting

- **`sync refused: ... exclusive lock`**: Joplin desktop holds the target
  lock. Close it (or wait for the 3-minute TTL) and retry.
- **`unsupported sync target version`**: the target was written by a newer
  Joplin than this build supports. Use matching versions.
- **Decryption failed / empty body**: the master password does not match,
  including case, spaces or a trailing newline. Do not trim it.
- **Port 41184 in use**: Joplin desktop's clipper owns it. Use 41185 for the
  app during the test; the Web Clipper scans 41184–41194.
- **A resource shows as ciphertext (`JED01…`) after a later sync**: fixed —
  a blob re-fetched for an already-decrypted resource is flagged encrypted
  again so the decrypt pass picks it up. An old build that still does this
  needs to sync once more with the master password set.
- **Wrong target path**: YAML uses `sync.local_dir`; Joplin's own
  `settings.json` uses `sync.2.path`. Both map to the same file-system target.

When the test passes, delete the test Joplin profile, the test folder and
`C:\joplin-lite-e2ee-test`, and unset the password variable.
