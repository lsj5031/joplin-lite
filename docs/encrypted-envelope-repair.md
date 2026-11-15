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
Official type-13 history envelopes with the complete twelve-column base schema
(and optional later `is_locked`) are validated and left byte-for-byte unchanged.
An incomplete revision still aborts; revisions never enter the repair manifest.
Both official revision schemas are independently derived from shipped migrations,
not inferred from user content.

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

## Dropbox repair operator

`repair-dropbox-envelopes` is a separate, explicitly selected operator command.
It does not run normal sync. It reads the selected official profile's target-7
setting and SQLite authorization through the same read-only credential helper
as native launch. Authorization stays in memory; no token/config is saved, no
OAuth grant or global authentication is changed, and no encryption password is
needed. Keychain access is unnecessary for this existing bearer authorization.
Other credential storage formats fail closed rather than starting a new login.

```sh
# Read-only remotely; creates a NEW private persistent backup directory.
gojoplin repair-dropbox-envelopes \
  --official-profile /absolute/official/profile \
  --backup /absolute/private/new-backup

# Only after independent manifest review and authorization:
gojoplin repair-dropbox-envelopes \
  --official-profile /absolute/official/profile \
  --backup /absolute/private/reviewed-backup --apply

# Read-only stable rescan, exact bytes/revisions/journal and unrelated inventory:
gojoplin repair-dropbox-envelopes \
  --official-profile /absolute/official/profile \
  --backup /absolute/private/reviewed-backup \
  --verify-scan /absolute/private/new-verification-directory

# Guarded rollback, only if explicitly intended:
gojoplin repair-dropbox-envelopes \
  --official-profile /absolute/official/profile \
  --backup /absolute/private/reviewed-backup --apply --rollback
```

Use the existing official Joplin app-folder OAuth context, not another account's
token cache. Preparation requires v3 `info.json` and an exact match between its
master key IDs and the selected official profile. It inventories every target
path/revision/content hash, downloads only root item metadata plus `info.json`,
and compares complete listings before/after scanning. Four read workers are
bounded by per-request timeouts and four attempts, with Retry-After honored.
Attachment payloads are not downloaded. All affected exact originals and the
manifest are flushed with private permissions before preparation succeeds.
The manifest contains SHA-256 before/after digests, Dropbox original revisions/
content hashes, target fingerprint and inventory cursor. Failed preparation
has no completed manifest and cannot apply. Keep failed backup directories for
diagnosis; use a new directory for another preparation.

Apply validates **all** backups first, checks target identity, then downloads
each candidate fresh before submitting only its reviewed correction with strict
Dropbox `update` mode and its original revision. Uploads are sequential, paced
at least 250 ms apart with an extra pause every twenty successes. No existing
byte, ciphertext, ID, internal timestamp, attachment, master key, lock or
`info.json` is changed. New Dropbox revisions are expected, not new note times.
Each upload, including timeouts/failed responses, is followed by an exact
download reconciliation before retry. Original bytes **and** original revision
permit a retry; repaired bytes with a new revision become a durable per-item
success journal. Any other state is downloaded/recomputed and stops for a new
reviewed manifest, never silently adopted or overwritten.

Apply uses a crash-releasing per-backup advisory lock on macOS/Linux (other
platforms refuse remote apply). Use only one operator, with other clients stopped where
feasible. CAS protects against writers that cannot be stopped. Success records
are atomically linked from flushed temporary files and their directory is
flushed; rerunning `--apply` reconciles already-finished items without writing
them again. Long Retry-After values stop the run instead of retrying early.
Rollback requires each recorded post-repair revision and exact repaired bytes;
it never rolls back a subsequent edit. It is not an automatic recovery action.
Verification saves a fresh stable manifest and `verification.json`, requires
zero affected envelopes, checks every repaired byte/revision against its success
journal, and reports any unrelated inventory changes for reconciliation.
It does not overwrite concurrent legitimate changes.

This command does not reset local profiles/cursors or force client re-import.
An equal internal timestamp can still cause a client to skip an update.
Real iPhone sync remains a separate verification, not implied by remote repair.

## Remote operation safeguards

Do not copy this directory over Dropbox wholesale. Lite's normal Dropbox
upload path uses unconditional `mode: overwrite`, not revision-conditional
updates. Using that path for repair could destroy concurrent edits.

1. Upgrade Lite first. Stop sync on **all** clients and stop any automated
   writers. Back up each client's local data and separately preserve unsynced
   changes, then take a complete raw target snapshot. A provider export must
   include a per-path record of Dropbox `rev` and content hash for every
   downloaded original. Record the listing cursor and reconcile all pages/
   continuations to obtain a stable inventory. A cursor alone is not a
   per-file concurrency token. The offline directory CLI does not acquire that
   inventory; the separate Dropbox operator does.
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

The original offline command never uploads. Remote execution requires an explicit
operator invocation and authorization, not merely installing the app.
Independent offline evidence and its exact-version limits are recorded in
`internal/models/testdata/official-envelope/README.md`.
