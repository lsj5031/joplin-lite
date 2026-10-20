# Dropbox sync efficiency and alpha release assessment

Reviewed 2026-10-20 (UTC+13), against published revision
[`fbf0d2a`](https://github.com/lsj5031/joplin-lite/commit/fbf0d2aff5c62211e2dff162bbbf8e3dbaf27bbe).
Measurements are from the native Windows app on Mercury with the existing
encrypted Dropbox library. This is one profile/network, not a general benchmark.

## Persisted changes and verified attachment reuse

Dropbox engine passes now persist the provider's listing cursor together with
a complete inventory in the active SQLite profile. After the first complete
listing, `/files/list_folder/continue` supplies changes across passes and app
restarts. Ordered file/folder/deletion events update the inventory, including
directory deletions and replacement by a file. Reconciliation still receives
the complete merged inventory; it is not given only the change feed.

A single SQLite value commits the inventory, provider cursor and pending
downloads together after **all** listing pages succeed. A failed/cancelled
listing or failed database write leaves that value unchanged and never reaches
local pruning. Pending items survive until the engine's applied-page cursor
acknowledges that inventory version. Listing success alone cannot lose an
unfinished download. Existing revision receipts continue to skip successfully
applied items after an interrupted pull. Changed files are selected from
Dropbox's feed rather than timestamp comparisons, so equal/older timestamps
cannot hide a changed remote revision.

An HTTP 409 tagged `reset` discards the cached base and rebuilds a complete
inventory. No partial replacement is committed if that rebuild fails.
Inventory and receipts are scoped by endpoint and hashed authorization identity;
access-token renewal keeps the scope when the refresh token is unchanged.
Neither credentials nor the official Joplin client's cursor are imported into
this inventory.

Attachment downloads can reuse `resources/<id>.crypted` or the normal resource
file only after matching the current Dropbox metadata's `content_hash`.
This is Dropbox's SHA-256 over the concatenated binary SHA-256 digests of
4 MiB blocks, **not** an ordinary file SHA-256. The lookup is read-only and
returns the exact verified bytes to the normal apply/decrypt path. Missing,
changed, unreadable, nonregular, oversized, or unverifiable caches fall back
to download. The actual profile comes from the engine, independently of the
token-cache location, including secure Keychain startup.

The first listing and uncached content still require network requests, and
verified ciphertext still needs normal application/decryption. Pass logs add
`cached_resources` alongside storage-request count; inventory logs identify
incremental versus full listings. Tests use disposable SQLite profiles and
loopback HTTP, covering restarts, older timestamps, cursor resets,
account/endpoint isolation, interrupted apply, partial listing/cancellation,
snapshot-write failure, directory changes, and matching/mismatched caches.

Protocol references: [Dropbox list-folder processing rules](https://github.com/dropbox/dropbox-sdk-go-unofficial/blob/main/v6/dropbox/files/client.go),
[cursor/reset definitions](https://github.com/dropbox/dropbox-sdk-python/blob/main/dropbox/files.py),
and [official content-hash implementation](https://github.com/dropbox/dropbox-api-content-hasher/blob/master/python/dropbox_content_hasher.py).

### Controlled comparison against the previous build

Compared with [the previous published build](https://github.com/lsj5031/joplin-lite/commit/ca3c97df58a3162eedc3fff5c64925db0d07a6b8),
the same public engine/SQLite/loopback fixture used 1,001 notes, one notebook,
20 resource records and 20 real FileV1 attachment envelopes. Both builds had
the same 15 ms delay per root response; nine quiet passes included one real
database close/reopen and new backend construction.

| Quiet pass | Previous build | Current implementation |
| --- | ---: | ---: |
| Full root listings | 1 | 0 |
| Root response pages | 3 | 1 |
| All storage HTTP requests | 10 | 7 |
| Note content downloads | 1 | 0 |
| Median elapsed | 60.289 ms | 24.489 ms |

These are synthetic results, not real Dropbox timings. With the same 20 valid
official-style `.crypted` files present initially, attachment downloads fell
from 20 / 5,651,820 bytes to zero. Same-size single-byte corruption forced all
20 correct downloads. Every final decrypted file was compared byte-for-byte
with independently generated plaintext, including an envelope larger than
the 4 MiB hashing boundary. Initial-pass samples did **not** establish a time
improvement; the cache evidence proves saved bytes/requests. A later batch of
503 events verified multi-page incremental addition/deletion handling, and
every remaining raw item and attachment still matched.

The older timing measurements below predate these two optimizations.

## Earlier follow-up implementation and Mac validation

The follow-up to this baseline now scopes Dropbox requests and inventory to
one engine pass. Delta and reconciliation reuse one **complete** listing;
it is discarded after success, failure or cancellation. Reconciliation and
cursor-boundary replay remain enabled. This is not persisted incremental sync.

Dropbox requests receive the pass's cancellation context. Lock release gets
a separate three-second cleanup budget, inside the native shell's five-second
shutdown wait. The tracing wrapper now forwards lock refresh as well as the
pass lifecycle. The engine logs completion after deferred cleanup, rather
than before it. Inventory logs include entries, pages and duration; pass logs
count storage HTTP attempts, including lock traffic but excluding OAuth.

Native status shows the active phase and previous successful time. Failures
no longer advance that time, and automatic passes wake/refresh the workspace,
including passes that finish between UI polls. Local-only mode stays idle.

Controlled before/after verification on Mars (macOS 27.0.1 arm64, Go 1.27.1)
used the same public engine, real SQLite and loopback Dropbox HTTP fixture:
1,001 notes, one folder, 20 resource records and 20 blobs, with 15 ms delay
per root-list page and nine timed quiet passes per build.

| Quiet-pass measurement | Baseline | Follow-up |
| --- | ---: | ---: |
| Median elapsed | 124.214 ms | 61.809 ms |
| Full root inventories per pass | 2 | 1 |
| Root listing pages per pass | 6 | 3 |
| Total storage HTTP attempts per pass | 13 | 10 |

That is a **50.24% reduction in this controlled fixture**, not a measured
speedup on real Dropbox. Note/blob downloads did not decrease: both builds
downloaded one boundary note, two `info.json` copies and zero attachment blobs
per quiet pass. Library/cursor/raw-item/blob digests remained unchanged.
An incomplete second page preserved state and retry recovered; cancelling a
held second page returned promptly, cleaned its lock and allowed a fresh pass
to discover a newly added old-timestamp note through reconciliation.

Mac validation also exercised the actual ad-hoc-signed `.app`: local API
startup, exact edit persistence, normal exit and reopening the same disposable
vault, with integrity checks and an inspected native screenshot after restart.
Editing in this packaged test used the app's API; widget typing/navigation
were separately covered through the real native view with `ui.Tester`.
The local CLI rehearsal passed plaintext and encrypted note/resource round
trips. Its stale locked-vault success expectation was replaced with checks
for the specific fail-closed error and an unchanged encrypted target.

The original defect probes fail on baseline and pass afterward. Root/shell
tests and sync/native race checks were also run. Live Mercury timing on the
new code, remote token expiry, packaged Windows onboarding and broad hardware
keyboard/accessibility testing remain unverified by this follow-up. No real
Dropbox files were modified during the Mac verification.

The historical observations and release assessment below describe the
pre-fix revision and explain the decisions; they are not current defect claims.

## Live Mac first-pass findings

On 2026-10-20, the original Mac profile's first Lite pass listed 7,599 entries
over 204 pages in 88.299 seconds, then processed 7,541 candidates. Without a
Lite cursor it downloaded existing content again: after about 17 minutes,
2,138 resource files had been written (464,053,812 bytes), with 875 raw-item
records. These are intermediate observations, not a successful completed pass
or a throughput benchmark. Avoiding this initial replay requires a separately
validated migration strategy; copying the official client's cursor is not an
established safe shortcut.

The pass also exposed two lock defects. All Lite instances used the filename
`locks/1_3_joplin-lite.json`, and a failed refresh only stopped the refresh
goroutine, leaving the sync running. Mercury completed its overlapping pass
at 12:48:11.539 UTC+13; the Mac reported an expired/missing lock at 12:48:42.578.
The timing is consistent with one instance deleting the other's shared lock,
but logs do not identify the actual deletion's actor or timestamp. The Mac
pass was stopped during pull, before the upload phase.

The local follow-up assigns each backend instance a unique lock identity and
cancels the pass if refresh fails, preserving that failure as the returned
error. Regression tests reproduce both defects on the published baseline and
pass with the fix, including cancellation of a blocked pull through the public
engine. Backend, native, sync race and S3-enabled sync tests pass. This does
not eliminate the separate first-pass download cost.

The downloading stage now reports `Downloading — X of Y`, counting only
successfully received/applied delta items, including attachment blobs. These
are item counts, not byte counts or an estimate of remaining time. For paged
targets `Y+` means only the pages seen so far are counted; the final page
removes the `+`. Later reconciliation, decryption and upload remain separate
phases, so finishing the download count does not claim sync has completed.

### Restarting an unfinished download

Dropbox downloads now record successfully applied item revisions in the
profile's `jl_sync_downloads` sidecar table. Restart still obtains a fresh,
complete remote inventory and reuses a receipt only if the filename and
Dropbox revision match within the same endpoint/credential scope. Resource
receipts also verify the current local file's SHA-256; missing or altered
files download again. Receipts contain revision identifiers and hashes, not
credentials or note bodies. Normal access-token renewal retains the scope
when a refresh token is available; reauthorization may safely cause a replay.

The sync cursor still advances only after a whole delta page succeeds, and
receipts are cleared after that cursor is saved. A crash before saving a
receipt may replay that item. A failed fetch/apply is never checkpointed.
Reconciliation, conflict handling, encrypted upload requirements and the
deletion guard remain in force. Targets without versioned receipts keep
their existing page-cursor behavior.

The download counter is reconstructed against the fresh inventory, counting
both verified receipts and newly applied items. It does not restore a stale
"Syncing" flag or promise that the previous total is still current. Tests
close and reopen real SQLite around an interrupted loopback Dropbox pull,
including a changed revision with an unchanged timestamp, missing/corrupt
attachment files, account/endpoint changes, token renewal and failed apply.
This is restart evidence with a synthetic service, not a forced interruption
of the owner's ongoing real Dropbox pass.

An older running binary has no receipts to reuse: installing the new bundle
does not retrofit its unfinished pass. Let that pass finish if possible;
checkpointed item reuse begins when the new binary runs. Receipts resume at
whole-item boundaries, not midway through an attachment's bytes. Native
remembered-profile/Keychain startup and persisted last-success time are
documented in [the shell README](../shell/README.md#remembered-native-startup).

## What the baseline logs show

The native log is `<data-dir>/desktop.log`, rotated to `desktop.log.1` on
startup when it exceeds 1 MiB. These measurements use sanitized timestamps;
raw logs can contain private item identifiers and should not be published.

| Pull start → `sync completed` (UTC+13) | Duration |
| --- | ---: |
| 10:52:25.745 → 10:55:04.241 | 158.496 s |
| 10:58:35.297 → 11:01:20.247 | 164.950 s |
| 11:03:42.795 → 11:06:25.720 | 162.925 s |
| 11:13:46.001 → 11:16:30.142 | 164.141 s |

All four completed passes had no error-level log entries; one additional pull
was interrupted by shutdown. The last completed pass breaks down as follows:

| Logged interval | Duration | Interpretation |
| --- | ---: | --- |
| Pull start → `processing delta` | 77.822 s | First full listing, JSON processing and delta preparation |
| `processing delta` → `delta summary` | 1.190 s | Fetch/apply delta candidates |
| `delta summary` → `server root children` | 82.046 s | Second full listing, JSON processing and reconciliation preparation |
| `server root children` → `sync completed` | 3.083 s | Reconciliation checks, encryption metadata, decryption and push |

The two listing intervals account for **159.868 s, or 97.4%** of the observed
164.141 s. These are intervals between log events, not HTTP-only timings.
Per-request latency, bytes, page count and retry count are not logged by the
native app, so their individual contributions remain unmeasured.

The later pass had one delta candidate, zero items to decrypt, 7,541 root
children, and no error or overlap-refusal logs. A read-only database sample
afterward showed no pending uploads or target-7 deletions. Do not describe
this as literally zero remote changes: the logs do not establish that.
In particular, `delta summary counts={}` counts only deletions/resource blobs
in the current code; it does **not** prove that no note metadata was applied.

Pull starts were 603.206 s apart, with 440.281 s between the previous
completion marker and the next pull. This matches the ten-minute scheduler,
not continuously overlapping passes. The idle, untouched footer showed
`Synced 11:16` at 11:18:42 and remained unchanged after a harmless repaint.
A permanently stuck `Syncing` indicator was not reproduced.

## Where the implementation spends work

1. **Two recursive inventories per pass.**
   [`getDelta`](../internal/sync/dropbox_backend.go) calls `listAll("")`,
   then filters entries against a timestamp cursor. Dropbox pagination
   cursors are used only within that listing, not retained across passes.
   [`Engine.Sync`](../internal/sync/engine.go) then unconditionally calls
   [`PullMissingItems`](../internal/sync/reconcile.go), whose children request
   calls `listAll("")` again. Pagination is sequential, at 500 entries/page.
   This is metadata listing, not redownloading every attachment. Lock
   listings and other requests are additional work, not included in a
   claim of exactly two total HTTP calls.
2. **Cursor-boundary replay.** `getDelta` includes timestamps equal to the
   saved cursor to preserve at-least-once delivery. A quiet pass can fetch
   an already-seen item. This is a deliberate correctness tradeoff; simply
   changing the comparison risks missing items with equal timestamps.
3. **Opaque status.** [`syncNoticeFor`](../shell/native.go) displays
   `Syncing…` for both pending and running work, without phase, progress or
   the last successful time. A minutes-long inventory therefore looks like
   a minutes-long upload. [`syncLoop`](../shell/backend.go) runs initially,
   on mutations/manual requests, every ten minutes, or after retryable errors.
4. **Cancellation does not reach Dropbox HTTP requests.** The backend's
   `do` creates requests without the engine context, with a 120-second
   client timeout, while native shutdown waits five seconds for sync.
   One actual shutdown logged `sync did not stop before shutdown`.
   The exact blocked operation was not captured; attributing that warning
   specifically to listing, lock refresh or lock release would be speculation.

`sync completed` is logged **before** deferred lock-refresh shutdown and lock
release. Timings above therefore exclude that cleanup, and do not prove the
footer's busy state ended at exactly the same timestamp.

## Recommendations recorded at baseline

- Carry cancellation through remote requests and verify close during a slow
  pass without closing SQLite beneath an active worker.
- Display checking/downloading/uploading phases and last successful sync;
  add sanitized phase duration and request-count diagnostics.
- Implement persisted Dropbox change cursors with a complete local remote
  inventory, deletion tracking and cursor-reset recovery. An incremental
  change feed alone cannot replace the complete inventory currently used to
  detect remote deletions. Test interrupted apply/replay and equal-time items.
  A pass-scoped shared inventory is a smaller possible optimization, but must
  preserve reconciliation and deletion safety; do not introduce an indefinite
  cache or disable reconciliation just to shorten the spinner.

Keep the current deletion guard, encrypted-upload requirements, conflict
preservation and interrupted-resource-path regression intact throughout.
Do not promise a speedup until the same real-vault timing is measured again.

## What is ready to publish

**Recommendation: a Windows alpha for opt-in early testers, after validating
the distributable and onboarding below; not a stable replacement claim.**

Completed evidence includes native encrypted Dropbox upload, independent
download/decryption with exact title/body, successful restart, unchanged
7,588 pre-existing remote non-lock files, and unchanged 7,382 local resource
paths/hashes. The owner additionally reports that official Joplin on their
phone received Lite's notes. That is useful real-client interoperability
evidence; it is user-reported, not an independently captured mobile test,
and does not establish every attachment/conflict/delete scenario.

Before distributing an alpha binary:

- Test the actual portable ZIP on a fresh Windows account. Mercury currently
  uses a private AppContainer launcher and secure credential bootstrap;
  [`package-windows.sh`](../shell/package-windows.sh) ships the executable,
  license, notices and README, not that setup. Do not publish private config,
  tokens, passwords, profile backups or personal logs.
- Provide explicit, reproducible Dropbox/E2EE setup, backup/restore steps,
  deletion-guard instructions and a safe first-run choice. The guard is opt-in
  in source, not a universal default. A token reused successfully on Mercury
  does not prove expiry/reauthorization works for new users.
- Reconcile release documentation with the current native UI and observed
  Dropbox results. The baseline README described the old mock web prototype;
  the follow-up replaces it with native screenshots and current setup/limits.
- State the long sync/checking time, shutdown warning and plaintext local
  password storage when saving native Settings. Mercury's encrypted bootstrap
  avoids that Settings path; it is not the generic packaged credential store.
- Publish a versioned source revision, portable artifact, checksums and known
  limitations, and make Windows signing status explicit. Verify update and
  rollback without exposing or replacing the user's library unexpectedly.

Before calling it stable, exercise real multi-client edits/conflicts, incoming
attachments and interrupted sync, token expiry/network interruption/sleep,
and deletion behavior on disposable remote libraries. Production deletion
propagation remains deliberately blocked in this owner's deployment. A macOS
release also needs its own packaged-build and signing/notarization validation;
Windows success does not supply that evidence.

No release was published by this review. This document records observations
and proposed work; it makes no runtime changes.
