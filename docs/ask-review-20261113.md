# Embedded Ask review — 2026-11-13

Reviewed the five Mitori commits from `6472c150f25b912570fad0b51ebac71ca6c76be5` through `4fa00396cb6f1f5f227786793465c411ddf2813f`, using isolated Linux and macOS worktrees. Accanto remains unchanged at `c5f7b1a6fc4281201b5f9eb293bf00281f48ab7a`.

## Corrections

- Reset actual Pi `pi*` tables, verified against real embedded storage.
- Persist scope per conversation; fail closed for legacy global scope and prevent wider context from reappearing after chat switching/restart.
- Persist and update the snapshot before exactly one terminal event; retain admission through finalization and prevent Stop racing initial writes.
- Dispatch events in order without synchronous UI callbacks under manager locks, avoiding the Stop/RunOnMain deadlock.
- Cancel runtime-owned storage and release orphan transactions before lifecycle writes and disposal.
- Honor explicitly empty credentials when testing or saving a changed endpoint.
- Revalidate search candidates and content against current scope on every page.
- Find the latest submission answer past 256 historical entries, including continuation pages, without returning an older answer.
- Retain full budgeted evidence passages and byte offsets for historical citations.
- Recompile source during bundle freshness checks; compare dist, embed and manifest without writing files.
- Guard stale idle timers against newer runtime ownership and report storage failures instead of announcing successful persistence.

Synthetic regression tests cover these changes. No personal notes, model credentials or GitHub keys were transferred.

## Fresh validation

Final corrective source passed:

- Linux: `CGO_ENABLED=0 go test ./... -count=1` and `CGO_ENABLED=0 go build ./...`.
- Mitori/macOS ARM64: `CGO_ENABLED=1 go test -race ./internal/assistant ./internal/store -count=1`.
- Mitori native shell: `CGO_ENABLED=0 go test ./... -count=1` and `CGO_ENABLED=0 go build -o "../tmp/Joplin Lite Reviewed" .` from `shell`.
- Mitori agent: clean `npm ci`, build, all **9 tests**, and `npm run check:bundle`.
- Focused native tests: history opens and inline citations navigate to the expected note. Ordered captures were generated with `ASK_REVIEW_CAPTURE_DIR=../tmp/ask-review-captures CGO_ENABLED=0 go test -run "TestAskHistoryAndInlineCitationSurface|TestAskInlineCitationOpensNote" -count=1 -v`.
- `git diff --check`.

Bundle SHA-256: `83e9f639b22c3e9f1c1f7141bfdf65e90994148fb4241c4489696a52baa0b135`, **1,710,894 bytes**, identical after clean builds on Linux and Mitori. A temporary symlink to another checkout's node_modules changed esbuild source-path annotations; a clean local installation resolved that verification artifact without changing source or lockfile.

Captures are on Mitori at `tmp/ask-review-captures/ask-cited-answer.png` and `ask-history-open.png` in the isolated verification checkout. These are synthetic MyGO headless-renderer captures, not a live provider session. The reviewing model cannot view images, so visual inspection is **unverified**; interaction assertions passed. The binary was built, not interactively smoke-tested.

## Plan reconciliation and remaining gates

The eight implementation stages in [the original plan](agentic-ask-moejs-plan.md#12-execution-sequence-and-acceptance-gates) are implemented to varying degrees; this review does not certify every acceptance gate as complete.

1. **Bundle/runtime:** real embedded Pi executes in deterministic integration tests; reproducibility and cgo-free backend builds verified.
2. **SQLite:** real Pi persistence, reset, transaction rollback/disposal and reopen/chat switching covered. Exhaustive fault injection is not claimed.
3. **Retrieval:** scoped search/read, exclusions, current-page revalidation and full evidence covered.
4. **Headless flow:** real embedded-agent loopback fixtures pass, including isolation after restart and citation handling.
5. **Lifecycle:** cancellation, terminal persistence/order, admission, credentials and orphan transactions covered. Full adversarial allocation/infinite-loop and every provider/network fault combination remain to be audited against the plan.
6. **UI:** shell suite, native build, history and citation interactions pass. Full ordered Send/search/read/stream/Stop/error/narrow-layout capture inspection remains outstanding; use the existing capture/demo harness with synthetic fixtures on an image-capable reviewer.
7. **Evaluation/footprint:** deterministic evaluation is included in the backend suite. [Existing live evaluation](ask-evaluation.md) belongs to the imported Mitori implementation and was not rerun for the corrected source. Cold-start, first-runtime latency, active/idle RSS, binary delta and repeated-run memory measurements remain unmeasured; run the plan's footprint comparison before claiming release readiness.
8. **Packaging:** committed bundle/manifest, notices and Ask CI exist; local checks above pass. Hosted CI, broader desktop portability and release-readiness gates must be checked separately.

Review and publication preserve the embedded moejs architecture, Go-owned scope/citations/network/credentials, read-only note tools, separate assistant SQLite and existing sync format.
