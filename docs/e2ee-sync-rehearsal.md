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

## CLI rehearsal (recorded evidence, 2026-10-18)

This leg was run once with a **scratch seeder kept in `tmp/` and not part of
the repository**, so the commands below are a record of what was verified,
not a copy-paste reproduction. The reproducible gate is the `go test` above;
for the config-file path, the Joplin desktop procedure in
[local-sync-test.md](local-sync-test.md) is the reproducible one.

The seeder created a local database that already had a KeyV1 master key, one
folder, one note and one resource. Then the regular CLI did the work, so the
run exercised the real config path (`sync.target`, `sync.local_dir`,
`api.master_password`).

1. Seed an encrypted owner:

   ```sh
   go run ./tmp/e2ee-seed tmp/e2ee-source 'test-password'
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
- Remote targets: Dropbox, S3 and Joplin Server still need their own
  real-target rehearsals.
- Conflict resolution under E2EE — the conflict path is covered by
  `internal/sync/conflicts_test.go` and the local sync test.
