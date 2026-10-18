# E2EE + sync rehearsal

This records the encryption/sync loop that runs **without** Joplin desktop:
one client owns a KeyV1 master key and pushes a note and a resource to a
File-system target (target 2); a second, empty client pulls the same target,
loads the key from `info.json`, and decrypts everything.

It is the fast gate before the Joplin desktop interop in
[local-sync-test.md](local-sync-test.md). It does not replace it: only Joplin
desktop proves that Joplin can read what we write.

## Automated coverage

```sh
go test ./internal/sync -run TestE2EE -v
```

This covers the engine-level loop: master key from `info.json`, encrypted
note and resource metadata (StringV1 envelopes), a FileV1-encrypted resource
blob, decrypt on pull, and the "edit keeps unknown metadata keys" round trip.

## Automated end-to-end run

```sh
bash qa/local-sync-e2e.sh
```

That script is the reproducible gate for this document: it pushes a plaintext
note between two throwaway vaults, then does the same with E2EE on (owner with
a master key, second vault joining with the password, plus a third vault
without the password that must not read anything), and checks the target files
for plaintext leaks. Everything it writes is under `tmp/local-sync-e2e`.

## CLI rehearsal (recorded evidence, 2026-10-18)

The commands below are the same run done by hand, and they are now
reproducible: the seeder lives in the repository as `tools/e2eeseed`, which
creates a local database that already has a KeyV1 master key (what Joplin
desktop creates when a user turns encryption on — joplin-lite joins an
encrypted library, it does not create one), one folder, one note and one
resource. The regular CLI then does the work, so the run exercises the real
config path (`sync.target`, `sync.local_dir`, `api.master_password`).

1. Seed an encrypted owner:

   ```sh
   go run ./tools/e2eeseed -data-dir tmp/e2ee-source -password 'test-password'
   ```

2. Push encrypted:

   ```sh
   export GOJOPLIN_MASTER_PASSWORD='test-password'
   ./tmp/gojoplin.exe sync --config tmp/e2ee-push.yaml
   ```

   `tmp/e2ee-push.yaml`:

   ```yaml
   sync:
     target: 2
     local_dir: "tmp/e2ee-target"
   api:
     token: "e2eetoken"
     key: "e2eekey"
     master_password: "${GOJOPLIN_MASTER_PASSWORD}"
   server:
     data_dir: "tmp/e2ee-source"
     port: 41891
   ```

3. Pull and decrypt into an empty database:

   ```sh
   ./tmp/gojoplin.exe sync --config tmp/e2ee-pull.yaml
   ```

## Observed results

Target after the push:

| Check | Result |
|---|---|
| `info.json` | `"e2ee": {"value": true}`, one `masterKeys` entry |
| Note `<id>.md` | `encryption_applied: 1`, envelope with `JED01` cipher text |
| Note body in the file | absent (`grep` count 0) |
| Resource blob `.resource/<id>` | starts with `JED01000` (FileV1) |
| Push log | `wrote sync info master_keys=1`, `sync completed successfully` |

Pull side:

| Check | Result |
|---|---|
| Pull log | `loaded sync info ... e2ee=true`, `decrypting items count=3` |
| Note row | `encrypted note \| the encrypted body \| encryption_applied = 0` |
| Resource row | `pic.png \| encryption_applied = 0 \| encryption_blob_encrypted = 0` |
| Resource file on disk | plaintext bytes (`encrypted resource blob…`) |

## What this does not cover

- Joplin desktop reading our output (the real interop gate; see the local
  sync test).
- Remote targets: Dropbox and S3 still need their own real-target rehearsals.
  The Joplin Server path has `qa/joplin-server-e2e.sh` (plaintext and E2EE).
- Conflict resolution under E2EE — the conflict path is covered by
  `internal/sync/conflicts_test.go` and the local sync test.
