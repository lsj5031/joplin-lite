# Encrypted sync envelope correction

Older Lite builds encrypted the full item correctly but wrote only the reduced
outer metadata keys. Official Joplin's reduced object is serialized through
the **full item schema**, including blank metadata placeholders (no title/body
sections). On an existing-item update, omitted `created_time` and
`user_created_time` can both be absent after parsing. The Synchronizer fallback
then assigns an undefined user creation time, causing SQLite's
`NOT NULL constraint failed: notes.user_created_time`. Blank timestamps instead
parse as numeric zero. The ciphertext contains the real creation/user times.

The corrected writer emits placeholders for notes, folders, tags, note-tag
links and resources, including fields not in Lite's typed models. It does not
put plaintext title, body, author, filename, OCR or unknown private fields in
the envelope. Resource blob encryption remains unchanged.

**Upgrading prevents new malformed writes. It does not rewrite already-synced
targets.** Unchanged notes are not re-uploaded, and changing timestamps or
editing content merely to force uploads is not a repair.

## Offline repair only

Use `gojoplin repair-envelopes` on an **exclusively owned, offline copy of a raw
sync-target directory**. This is not a JEX export, a Joplin profile/database, a
live filesystem-sync target, or a directory managed by the Dropbox desktop
client. Do not point it at actual live data. No config, profile, key, network
or sync backend is loaded. Build the root CLI with `go build -o /absolute/path/gojoplin .`.

```sh
# Lists affected IDs and count only, writes nothing.
/absolute/path/gojoplin repair-envelopes /absolute/path/offline-sync-copy

# New backup directory must be outside the copy; all originals and the
# checksum manifest are flushed before the first replacement.
/absolute/path/gojoplin repair-envelopes /absolute/path/offline-sync-copy \
  --apply --backup /absolute/path/new-envelope-backup

# Preview rollback, then restore only if current bytes still match the repair.
/absolute/path/gojoplin repair-envelopes /absolute/path/offline-sync-copy \
  --rollback /absolute/path/new-envelope-backup
/absolute/path/gojoplin repair-envelopes /absolute/path/offline-sync-copy \
  --rollback /absolute/path/new-envelope-backup --apply
```

The tool considers only root-level `<32 lowercase hex>.md` items. Plaintext
and complete encrypted items are left unchanged; `.resource/`, `info.json`,
locks and other files are not modified. Unsupported encrypted item types,
malformed metadata, duplicate keys, invalid IDs, title/body sections,
inconsistent encryption flags and invalid/unsupported JED string framing
abort preflight before writes. Unrecognized metadata is retained byte-for-byte,
not printed. Framing validation does **not** authenticate ciphertext without
keys. The operator must obtain the snapshot from the correct target.

Correction only prepends absent blank metadata lines. Every existing byte,
including ciphertext, update time, routing fields and unknown metadata, stays
unchanged. No attachment is decrypted or re-encrypted. Backups contain exact
original bytes with a versioned before/after SHA-256 manifest and private
directory/file permissions. Keep them protected as sync data.

Apply and rollback use per-file atomic replacements and compare current
bytes immediately before writing. They are **not a multi-file transaction**.
If interrupted, retain the backup. Re-run repair with a new backup directory
for remaining files, or rollback with the original manifest; already-original
files are skipped. A complete repaired directory is a no-op on rerun. Rollback
refuses changed files or altered backups. There must be no concurrent writer:
the local compare/rename sequence is not a remote compare-and-swap or a lock
against other programs. A crash can leave a harmless `.envelope-*` temp file.

## Staged remote application plan, not implemented by this tool

Do not copy this directory over Dropbox wholesale. Lite's normal Dropbox
upload path uses unconditional `mode: overwrite`, not revision-conditional
updates. Using that path for repair could destroy concurrent edits.

1. Upgrade Lite first. Stop sync on **all** clients and stop any automated
   writers. Back up each client's local data and separately preserve unsynced
   changes, then take a complete raw target snapshot. A provider export must
   include a per-path record of Dropbox `rev` and content hash for every
   downloaded original. Record the listing cursor and reconcile all pages/
   continuations to obtain a stable inventory. A cursor alone is not a
   per-file concurrency token. This CLI does not acquire that inventory.
2. Run dry-run and apply on a second offline copy. Retain untouched snapshot,
   local repair backup and provider revision inventory. Independently compare
   every manifest entry: only missing blank lines were added; ciphertext and
   existing timestamps are identical. Validate import using disposable
   clients before authorizing live application. This run performed no such
   live validation.
3. A separately reviewed, explicitly authorized provider uploader must upload
   **only** manifest-listed `.md` corrections with Dropbox's strict
   revision-conditional update mode (`{".tag":"update","update":originalRev}`,
   `autorename:false`, `strict_conflict:true`). Immediately before each upload,
   re-download and confirm original bytes/digest and revision. On any conflict
   stop, fetch the new item, and recompute the correction; never fall back to
   overwrite, add, rename or timestamp changes. Do not change `info.json`,
   master keys, locks, deletions or `.resource` blobs.
4. Persist each success's new revision/hash. After a timeout or retry, inspect
   remote state: if exact repaired bytes are present, record success; if exact
   original bytes/revision remain, retry the conditional write; otherwise stop
   for reconciliation. Do not blindly retry an old revision or use a target
   cursor as an upload precondition. A new content revision must enter Dropbox's
   normal change stream even though the note's internal update time is retained.
5. Resume one official client first and verify counts, errors, decryption,
   original timestamps and attachment bytes before others. A new Dropbox
   revision does not guarantee re-import: an official client may already have
   advanced its cursor, or may skip equal internal update times. Retrying sync
   may suffice for a previously failed update, but do not promise it. If items
   are still skipped, use the official client's supported re-download/rebuild
   procedure only **after** backup and unsynced-change reconciliation, first
   in a disposable profile. Do not edit SQLite, fabricate update times, reset
   cursors by hand or delete live local data as part of this tool.
6. Remote rollback likewise needs conditional updates against the **recorded
   post-repair revisions**, restoring backed-up bytes only where the remote
   item is still exactly the repair. Never roll back over later edits.

This change supplies no remote uploader and grants no authorization to perform
these live steps. No real Dropbox/iOS test was run. Independent offline evidence
and its exact-version limits are recorded in
`internal/models/testdata/official-envelope/README.md`.
