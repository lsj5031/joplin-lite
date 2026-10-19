# Real S3 test (sync target 8)

This is the gate for the S3 path: a **real** S3-compatible object store, three
throwaway vaults and one bucket. The default binary carries an S3 *stub*, so the
run also proves the build tag is what decides the backend.

Covers: the `-tags s3` build, `info.json` on a fresh bucket (`NoSuchKey` is not
a failure), the delta/children listings over `ListObjectsV2`, item upload
(`/api/items/root:/<name>:/content`), the flat-file lock protocol under
`locks/`, and the E2EE push/decrypt path.

## 1. Run it

```sh
bash qa/s3-e2e.sh
```

The script does everything: starts `rustfs/rustfs` (Apache-2.0, S3-compatible)
in Docker on `127.0.0.1:19000`, creates an empty `joplin-e2e` bucket, builds
both binaries, runs the legs, and removes the container again. It needs the
`aws` CLI on the host — the app talks S3 itself, and `aws` is only used to
create the bucket and inspect objects from outside.

Point it at a store you already have instead of starting RustFS:

```sh
S3_URL=https://s3.example.com S3_BUCKET=my-bucket \
AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... bash qa/s3-e2e.sh
```

Anything S3-compatible works (AWS S3, MinIO, Cloudflare R2, …); use a bucket
that is empty or disposable, because the script empties it before it starts.

## 2. What it checks

1. **The stub build.** The same vault config synced with the default binary must
   fail with `S3 sync target is not compiled into this build` — the driver is
   opt-in at build time, not silently missing.
2. **Plaintext.** Vault A creates a notebook and a note through the clipper API
   and syncs; the bucket must hold `<note-id>.md` with the body in the clear;
   a fresh vault B pulls it back and serves the same body.
3. **E2EE.** A seeded owner vault (a `KeyV1` master key, like Joplin desktop
   after encryption is turned on) pushes the note and a resource. In the bucket
   the note must carry `encryption_applied: 1`, `info.json` must record the
   `e2ee` section, and neither the note nor `.resource/<id>` may contain
   plaintext. A vault with the master password decrypts note and resource; a
   third vault synced without it must never expose the body.

The script prints `PASS` with the note and resource ids, or `FAIL` with the
reason. Everything it writes is under `tmp/s3-e2e`: the vaults, their configs,
their logs and the objects it pulled back for inspection. Delete the directory
when done.

## 3. Reading a failure

The object store is the only thing the vaults share, so a mismatch is always in
the wire format or the backend:

* `sync failed: push sync info failed: ... NoSuchKey` — a fresh bucket's missing
  `info.json` is being treated as an error instead of an empty target.
* `the bucket's note has no plaintext body` — an encryption key is active in a
  plaintext leg, or the item was written under a different key.
* `plaintext leaked into the bucket's note` — the push skipped encryption.
* `the resource blob did not survive the round trip` — the blob was re-fetched
  and not re-decrypted, or the `.resource/` key was not written.

Objects can also be inspected by hand while the container is up:

```sh
AWS_ACCESS_KEY_ID=rustfsadmin AWS_SECRET_ACCESS_KEY=rustfsadmin \
  aws --endpoint-url http://127.0.0.1:19000 s3 ls s3://joplin-e2e/
```
