# Embedded moejs Ask/Search — implementation and execution plan

Status: implementation plan, 2026-11-13. Selected architecture: native MyGO UI + embedded moejs + Accanto-derived Pi AI/Pi Durable agent. This document authorizes no deployment or publication; the requested deliverable is the plan.

## 1. Outcome and scope

Add an Ask panel to joplin-lite that can answer questions across selected notes by searching, reading passages, refining searches and returning clickable citations. Preserve the native Go application, cgo-free builds, existing Joplin sync compatibility and lossless note serialization. Ship the JavaScript agent inside the application binary; Node is a build tool only.

Example: “What did I decide about the camera repair, and what follow-up is outstanding?” Ask searches several relevant terms, reads evidence from multiple notes, identifies contradictions or missing information, and returns a concise answer with citations that open those notes.

The architecture is selected. Early compatibility work implements and fixes the actual bundle/host integration; it is not a decision to abandon moejs or rewrite the agent in Go. Report specific SDK gaps and patch the smallest host adapter needed. A change of architecture requires an explicit decision.

Version 1 includes text notes, multi-turn conversations, one active Ask run application-wide, current-note/notebook/all-notes scope, one initial tool-capable provider API, local history, streaming, cancellation and citation verification. The initial API is OpenAI-compatible Chat Completions, matching an existing Accanto path. A second provider adapter is a subsequent vertical slice using the same bridge. No embeddings, public web search, arbitrary user-installed plugins, autonomous note writes, attachment interpretation or model-generated Code Mode in v1. Those can extend this architecture after retrieval evaluation.

## 2. Grounding and reusable implementations

Pinned source inspected:

- joplin-lite: 6472c150f25b912570fad0b51ebac71ca6c76be5.
- pi-accanto: c5f7b1a6fc4281201b5f9eb293bf00281f48ab7a; Pi packages 1.4.0.
- MoeForge/moejs: 934171643e14a785a9c03e8bee10f82e5e1de478.

Recheck main and preserve local changes when implementation begins. Pin moejs to this commit or a reviewed release containing it; record the resolved Go pseudo-version and go.sum. Do not float the engine or Pi packages.

Existing Joplin seams: service/service.go, internal/store/summaries.go, internal/store/search.go, shell/native.go, shell/workspace.go, shell/backend.go, shell/deeplink.go. The native shell already uses mygo.RunOnMain and invalidation for asynchronous results. PRODUCT.md reflects the current native UI; parts of INTENT.md still describe the former webview.

Reuse Accanto's provider/model registration and Harness/registry/tool setup from panel.js; evidence catalog and bounded reads from attachments.js; search/read/error behavior from research.js; relevant fixture scenarios from test-research.mjs. Record originating paths, SHA and MIT attribution in agent/UPSTREAM.md. Extract only the needed code into Joplin initially; do not make Accanto depend on a new shared package in the first delivery. Keep later extraction possible by avoiding DOM/Chrome imports in the portable agent entry.

Important discovery: Pi Durable already exports portable SqliteStorage from @earendil-works/pi-durable/storage/sqlite. Retain its schema, migrations, commit ordering and conversation/task/document implementation. Supply the small SqliteDatabase interface from @earendil-works/chord/sqlite through Go. Do not port the full Storage interface or emulate IndexedDB.

moejs provides Compile/NewRuntime, eventloop.New, eventloop promises and webapi.Install. Fetch streams and cancellation are supported; DOM, Chrome, IndexedDB, Worker, Blob, File and FormData are not supplied. Text-only model requests deliberately avoid attachment/upload paths. Inspect transitive SDK initialization too: lazy upload methods must not fail at import time. Browser bundling alone does not prove compatibility.

## 3. Architecture and ownership

```text
Native Ask panel
  -> Go assistant manager
  -> moejs event-loop goroutine
  -> bundled agent entry / Pi Durable Harness / Pi AI provider
  -> Go-owned provider HTTP transport

Agent tools
  -> Go note broker
  -> existing Joplin service/store

Pi SqliteStorage
  -> JavaScript SqliteDatabase facade
  -> Go assistant database worker
  -> separate assistant.sqlite using modernc.org/sqlite

Agent view/events
  -> Go event reducer and bounded event queue
  -> mygo.RunOnMain
  -> native UI state and Invalidate
```

Ownership rules:

1. Go owns vault identity, scope authorization, model endpoint, credentials, budgets, run identity and shutdown.
2. JavaScript owns Pi orchestration, typed model tools, prompt construction, conversation view projection and the portable SQLite storage implementation.
3. One event-loop goroutine accesses one runtime. Runtime values never cross goroutines; host workers receive detached validated Go data and settle results back on the loop.
4. A single manager admits one active run across conversations in v1. Chat switching can display another saved chat, but cannot start a competing run. Run ownership is not inferred from which note is selected.
5. The agent receives no note mutation methods or vault SQL connection. Its database facade reaches only assistant.sqlite, with no ATTACH/extension loading or arbitrary external paths. This is a bundled first-party plugin, not a general plugin marketplace.
6. Compile the embedded module once, create the runtime lazily when Ask is first used. Keep one runtime while Ask is active; after a configurable idle period (initially 60 seconds), flush, close and discard it. Reopen from SQLite on demand. Do not pool runtimes initially.

## 4. Proposed file layout

Use existing repository naming conventions; filenames below are implementation targets, not claims that APIs already exist.

```text
agent/
  package.json, package-lock.json
  build.mjs
  src/entry.ts              bootstrap, open, submit, abort, close
  src/host.ts               versioned host declarations and validation
  src/models.ts             Accanto-derived provider/model registration
  src/tools.ts              read-only note tool schemas and calls
  src/instructions.ts       evidence and citation instructions
  src/sqlite.ts             portable database facade
  src/events.ts             Pi view -> native event projection
  test/                     JS fixtures for portable behavior
  dist/agent.js             reproducible embedded release artifact
  dist/manifest.json        API version, package versions, source hash
  UPSTREAM.md

internal/assistant/
  manager.go                lazy lifecycle, conversations, active run
  protocol.go               requests, scopes, events, errors
  runtime.go                moejs setup, loop ownership, module calls
  network.go                endpoint restriction, credentials, streaming
  notes.go                  scoped read/search broker
  evidence.go               versioned passages and citation validation
  database.go               assistant SQLite worker and tx handles
  budgets.go                resource accounting
  bundle.go                 embed agent artifact from local bundle directory
  bundle/                   generated copies for go:embed
  *_test.go

internal/store/assistant_search.go
internal/store/assistant_search_test.go
shell/ask.go                 panel state/layout/actions
shell/ask_settings.go        endpoint/model/credential controls
shell/ask_test.go            native interaction and screenshot scenarios
shell/backend.go             manager ownership and shutdown wiring
shell/native.go/workspace.go Ask entry point and panel placement
cmd/ask_check.go              deterministic developer diagnostic, if useful
qa/ask/                      fixtures, evaluation manifest and reports
```

Go embed patterns cannot reference parent directories. The build script copies its verified output into internal/assistant/bundle before Go compilation. Commit the release bundle and manifest so Go-only builds need no Node; CI rebuilds and checks exact consistency. Avoid two independently maintained copies.

## 5. Host protocol

Define a single host global, joplinHost, with apiVersion: 1. Validate all JS-to-Go inputs, cap message sizes and reject unknown discriminants. Keep capabilities separate; do not create a generic RPC to every service method.

Proposed TypeScript contract:

```ts
interface Host {
  apiVersion: 1;
  run: { id: string; conversationId: string; scope: Scope; limits: Limits };
  notes: {
    search(input: SearchRequest): Promise<SearchPage>;
    read(input: ReadRequest): Promise<NotePassage>;
    listNotebooks(): Promise<NotebookSummary[]>;
    listTags(): Promise<TagSummary[]>;
  };
  db: AssistantDatabaseHost;
  emit(event: AgentEvent): Promise<void>;
  uuid(): string;
}

type Scope =
  | { kind: 'note'; noteId: string }
  | { kind: 'notebook'; notebookId: string; includeDescendants: boolean }
  | { kind: 'vault' };
```

Go creates the scope; tool arguments cannot widen it. Tag filters intersect it. Notebook descendants are explicit; a notebook-only selection is exact by default. Changing scope starts a fresh conversation in v1, because old messages may already contain evidence from a wider scope. Revalidate current membership before each note read; a note moved out of scope cannot be read merely because it appeared in old results.

Proposed Go service contract:

- Start(ctx, AskRequest) -> RunHandle or Busy/validation error.
- Cancel(runID) -> idempotent acknowledgement.
- Snapshot(conversationID) -> read-only projection.
- ListConversations(cursor), DeleteConversation(id).
- Close(ctx) -> stop admission, cancel, drain, close.

Every event carries apiVersion, conversationID, runID and monotonically increasing sequence. Events: run.started, activity.updated, answer.delta, answer.snapshot, evidence.read, citation.resolved, run.completed, run.cancelled, run.failed. Terminal events occur once per run. Progress means observed activities, never invented completion percentages.

Prefer Pi's existing view subscription as the source of assistant text/state. Implement a small reducer/diff to emit deltas or snapshots. Check exact view shapes during implementation; do not assume provider chunks equal durable commits. Persist final citation links and outcome as application entries/documents using Pi's supported extension mechanism.

Use stable structured errors: invalid_request, out_of_scope, note_missing, note_changed, encrypted_content, budget_exceeded, provider_auth, provider_rate_limit, provider_unavailable, storage_failed, cancelled, runtime_failed. Preserve diagnostic causes in private logs without credentials or note bodies. A recoverable missing note should allow the agent to report incomplete evidence; a storage failure terminates the run.

## 6. moejs integration

Build the selected agent dependency graph with esbuild, ESM, browser target and an explicit supported language target. Define process as undefined as Accanto already does, but only after confirming the selected adapter's environment paths. Fail on unresolved node:* imports; include the metafile and package inventory. No runtime npm resolution, remote module fetching or filesystem module loader. Only the embedded bundle is loadable.

Initialize runtime, set a bounded MemoryLimit, create eventloop, install webapi with the managed HTTP client, then install host functions and load the compiled module on the owner loop. Keep shared frozen builtins. Add only demonstrated missing globals, for example crypto.randomUUID backed by Go; do not fake cryptographic operations or disable compatibility checks with empty stubs.

Expose bundle functions with versioned input objects: open(config), submit(request), abort(runID), snapshot(conversationID), close(). These are proposed exports. Internally follow Accanto's actual Harness.open, root.configure, root.submit({..., requestId}), submission.wait and root.abort flow. Use a Go-generated UUID as requestId so retries with the same accepted request do not create duplicate submissions. Verify idempotency with the real Pi contract; never automatically retry a failed streaming model call.

For asynchronous note/database calls, create a promise via loop.NewPromise while on the loop, dispatch detached work, and call the settle function with a conversion callback. Only that callback touches runtime values. Bound host worker concurrency and use QueryContext/ExecContext for database work.

Watch the event-loop result and unexpected promise rejections. Fatal runtime errors invalidate the runtime; close it and reopen from durable data for a later user request. Never reuse a runtime with pending work after hard cancellation.

Networking: webapi.Install uses the managed client and api.SetContext(runContext). Closing/terminating the loop alone does not cancel fetch; api.Close or context cancellation is also mandatory. Loop.Interrupt wakes an idle loop; Runtime.Interrupt by itself does not. Never call blocking Stop/Terminate on the loop's own goroutine.

moejs notes that a panic in a posted host task can unwind through its loop. Make all host callbacks return errors; contain panics at owned worker/loop boundaries, classify runtime_failed and clean up resources. Do not swallow them or continue a potentially inconsistent run.

## 7. Provider connection and credentials

Start with the existing Accanto Chat Completions adapter; retain its parsing, tool call normalization and streaming implementation. User settings: API format, base URL, model ID, context limit, maximum output, and key persistence choice. Model profiles must reflect actual model capabilities; do not blindly copy Accanto's large default token limits.

Go stores secrets using an available OS credential facility; if unavailable, offer session-only keys as the guaranteed path. Persistent non-key model settings can live in assistant metadata. Keys never enter synced notes, JavaScript durable entries, logs or exported chats.

Preferred transport boundary: provide the SDK a non-secret placeholder where its constructor requires a key, then let Go's endpoint-scoped RoundTripper replace the relevant authentication header. Cover the initial provider with explicit tests. Preserve provider-specific auth header rules as additional APIs are added. Never accidentally transmit the placeholder or credentials to a nonconfigured destination.

Permit only the configured scheme, host, port and API base path. Allow explicitly configured local HTTP inference endpoints. Reject redirects in v1. Use a fresh dedicated transport/client without ambient cookies, unrelated credentials or arbitrary proxy inheritance unless existing app configuration explicitly requires a proxy. No public-web fetch tools are installed. Bound request/response bytes and use context plus separate connection/header/inactivity timeouts suited to streaming.

Connect/Test sends a small explicit model request and verifies a response; catalog access alone is not successful inference. Production live checks use the user's chosen endpoint only when authorized. Automated CI uses a local deterministic fixture server, never user keys.

## 8. Pi Durable storage through Go

Create assistant.sqlite next to the app's existing data directory using restrictive permissions, WAL and a bounded busy timeout. It contains conversation history, task state, excerpts already sent to models, evidence metadata and model settings. It is plaintext local data, like the note store, but is not synced. Keep lifecycle metadata in a separate jl_ask_* table namespace; Pi migrations own their own namespace and schema version.

Implement the portable interface checked against installed declarations:

```ts
interface SqliteExecutor {
  exec(sql: string): Promise<void>;
  run(sql: string, ...params: SqliteValue[]): Promise<void>;
  get<T extends object>(sql: string, ...params: SqliteValue[]): Promise<T | undefined>;
  all<T extends object>(sql: string, ...params: SqliteValue[]): Promise<T[]>;
}
interface SqliteDatabase extends SqliteExecutor {
  transaction<T>(callback: (tx: SqliteExecutor) => Promise<T>): Promise<T>;
  close(): Promise<void>;
}
```

Values are null, number, bigint, string and Uint8Array. Implement binary and integer conversions deliberately; avoid lossy JSON conversion of bigint and preserve undefined for a missing get row. Reject integers that cannot be represented by the driver. Test null, empty strings, bytes, large IDs and affected error paths.

JavaScript transaction(callback) obtains an opaque Go transaction handle, invokes the callback with an executor bound to that handle, commits on resolution and rolls back on rejection. Preserve the callback error after successful rollback; a failed rollback must surface an aggregate/distinct error. Invalidate handles on commit/rollback/close and bind them to their owning runtime generation.

Use a single database scheduler. While a transaction is active, transaction-scoped operations are admitted; unrelated database operations and other transactions wait. Do not block the scheduler on an unrelated queued request ahead of a transaction's next statement: route tx work separately or use an admission gate before enqueue. A naive FIFO worker deadlocks here. Never hold a Go mutex while awaiting a JS callback. Every request has context cancellation and size limits; DB work cannot call back into JS while holding driver locks.

Use a dedicated owned connection/transaction for this facade; generic pool calls must not escape the active transaction. Block ATTACH/DETACH and extension loading; if SQL validation is introduced, use SQLite authorization/driver support or a constrained execution design rather than regex alone. No source path or database token supplied by the model can select the vault DB.

SqliteStorage.open performs upstream migrations. Test migration rollback and unsupported newer versions. Preserve a recoverable pre-upgrade database snapshot before nontrivial schema upgrades, respecting WAL consistency via SQLite backup/checkpoint-safe procedures. Never copy only the main DB file while WAL writes are active.

One manager owns the assistant DB for the process. If the existing application permits multiple processes on one profile, detect/lock assistant ownership or decline Ask in the second process; Pi ID allocation and transaction semantics must not be assumed safe across independent writers.

## 9. Retrieval and evidence

Add a dedicated assistant search query, keeping ordinary list sorting unchanged. Use the existing FTS5 index and safe token quoting, but rank candidates using bm25(jl_notes_fts) with a stronger title weight and id as a deterministic tie-breaker. Inspect actual index column order before selecting weights. Use parameterized MATCH and filters. Return a small bounded snippet, title, notebook and timestamps; never whole bodies for search results.

Proposed request: query <= 400 characters, limit default 8/max 20, optional tag and cursor. Scope is implicit host state. Notebook descendants are resolved by Go. Exclude conflict copies, trashed/deleted and encryption-applied notes unless a future explicit scope enables them. Existing methods do not establish all these filters, so enforce them in the assistant query and read path.

For ranked result pagination, initially materialize up to 50 candidate IDs and scores per query in a bounded per-run cache. Return opaque cursors bound to scope, query hash, run generation and offset. Revalidate notes on each page/read. This avoids pretending the existing timestamp cursor describes BM25 ordering. Expired cursors restart the search explicitly. Do not hold long-lived read transactions over an entire model run.

read_note accepts noteId, optional expectedVersion, startByte (UTF-8 byte offset) and maxBytes. Defaults: 6 KiB, maximum 12 KiB. Go clamps boundaries to valid UTF-8 and returns actual start/end, nextByte, line range, updated timestamp, version hash and evidence ID. A huge single-line note remains readable in bounded chunks. Missing or changed notes are explicit results, not empty evidence.

Keep per-run evidence registry entries keyed by an opaque ID. A passage contains note ID, vault identity, content version, byte range, title and exact excerpt. Version uses a hash of title/body plus relevant identity rather than updated_time alone. Search snippets are candidate evidence; final citations must resolve to passages read through read_note. Record the excerpt already shown to the model so a subsequent note edit cannot silently rewrite the historical source.

Instruct the model to cite opaque passage tokens, for example [E7]. Parse only the supported citation syntax and resolve it through Go's registry. Unknown tokens remain visibly unverified, never clickable. Citation links navigate internally to the note and passage when the version still matches. If changed, show the saved cited excerpt and “Note changed since this answer”; do not highlight an unrelated current range.

The model may broaden or split queries, search synonyms and follow note links only via the scoped reader. Pin the selected note snapshot when the user explicitly asks about an unsaved draft: either save using the existing user edit path first or offer a clearly labeled transient selected-note source. Never silently ignore unsaved editor content. The agent itself receives no save operation.

## 10. Budgets, cancellation and recovery

Initial engineering defaults, to be measured and adjusted (not claims about current behavior): one active run; 12 model turns; 40 note-tool calls; 120 KiB cumulative emitted note evidence; 20 search results/page; 50 cached candidates/query; 12 KiB/read; 120-second run deadline; 128 MiB JS allocation limit. Add a model-context budget computed from the selected profile and reserve requested output plus tool overhead. Bytes are not token counts: use the provider tokenizer when available or a documented conservative estimator, and reject/truncate before constructing oversized requests.

128 MiB is an initial runtime allocation setting, not a total RSS guarantee. Go buffers, SQLite, HTTP and UI caches have separate caps. Inventory moejs memory accounting/reset semantics at the pinned version and test that asynchronous tasks cannot reset away the run-level evidence budget. Track cumulative limits in Go regardless of engine task resets. Also enforce a cumulative request-token budget across all turns (initially 64,000 estimated input tokens and 8,000 output tokens per run); a per-call context window does not bound total spending. Account for repeated history in every request and disable SDK retries initially so retry costs cannot bypass accounting. Expose observed usage when the provider supplies it, with estimates labeled.

Stop sequence: atomically mark cancellation requested; cancel run context and provider fetch; post Pi abort while the loop is responsive; allow bounded cleanup; if necessary Loop.Interrupt, then webapi.Close and loop.Terminate from the supervisor; await host worker completion/rollback; write one cancelled outcome and retain partial text with a stopped label. Cancel-before-submit and cancel-after-terminal are harmless. A cancelled run's late events are rejected by run ID/generation.

Persist accepted user submissions with stable request IDs before provider work. Normal completion persists final outcome before emitting terminal success. On application restart, reconcile orphaned running submissions as interrupted; do not automatically replay inference or tools. Offer explicit Retry as a new request linked to the old one. Durable history and automatic resumption are separate features.

Shutdown: stop new Ask admission, cancel active Ask, close Pi/harness while possible, cancel network, terminate/discard runtime, drain/rollback assistant DB work, close assistant DB, then proceed to existing backend vault DB close. Use bounded shutdown and report failures. Do not close the vault store while an admitted note read can still use it.

## 11. Native UI integration

Add an Ask action to the native workspace and a collapsible panel; preserve editor width with a minimum size and use a dialog/full-width panel in narrow windows. Do not build an HTML renderer. Reuse existing native Markdown and link handling for answer display, with citation targets resolved by the host.

Panel: scope control, conversation/history control, message list, activity row, multiline composer, Send/Stop and source citations. States: unconfigured, ready, queued/starting, searching, reading, streaming, stopped, failed with retry, no evidence and complete. Current scope remains visible. Show “Reading 3 notes” only after observed reads, not planned model calls.

Background code produces detached events. Coalesce answer deltas at most every ~33 ms into a bounded queue; deliver state changes with mygo.RunOnMain, then Invalidate. Keep lossless durable/terminal events separate from coalescible UI deltas. If queue pressure grows, use a cumulative answer snapshot rather than dropping text or blocking the event loop indefinitely. Enforce a maximum answer size and generation checks after chat switches.

Settings use existing native form patterns. Explain at connection time that selected decrypted note excerpts are sent to the configured model provider even when Joplin sync uses E2EE, and describe and show session-only credential status. Delete-chat cancels its run, removes durable chat data and evidence metadata, and releases in-memory caches. SQLite logical deletion does not promise forensic erasure; document backup retention accurately.

## 12. Execution sequence and acceptance gates

Each numbered unit should land as a reviewable commit or small PR. No merge/deploy is implied by this plan. Implementation proceeds through the gates with concrete fixes, preserving completed units.

### 1 — Bundle and embedded runtime

Add agent package/lockfile, exact moejs dependency, reproducible esbuild artifact and embedded runtime runner. Bundle a real Pi adapter plus minimal harness imports, not a substitute hand-written HTTP client. Load with CGO_ENABLED=0, create a host promise, stream a fixture response, call a Go tool and shut down. Add missing SDK initialization shims only with a reproducer. Gate: actual dependency graph executes; bundle has no unexpected Node/Chrome/IndexedDB/Worker imports; loop teardown leaves no ongoing request.

### 2 — Portable SQLite bridge

Implement async executor/tx facade over modernc and open upstream SqliteStorage. Use upstream schema/migrations unchanged. Exercise commit, rollback, concurrent unrelated operation ordering, callback failure, rollback failure, close during read, reopen and cursor scans. Gate: a real Pi conversation/task survives runtime disposal and DB reopen, and transaction isolation survives interleaving.

### 3 — Scoped retrieval broker

Add ranked FTS query, cursor cache, bounded reads and evidence registry. Seed synthetic notes with exact terms, synonyms requiring reformulation, large Unicode bodies, conflicting/outdated facts, trash, conflicts, encrypted items and disjoint notebooks. Gate: scope exclusions hold through search, paging and read; edits/deletion during a run surface correctly; normal note-list behavior remains green.

### 4 — End-to-end headless agent

Extract Accanto model registration/tool setup into agent sources; wire Harness.open + SqliteStorage + read-only tools + submit/wait/abort. Add instructions and citation projection. Use a deterministic local model server returning a sequence: search call -> no matches -> refined search -> note reads -> cited answer. Gate: actual streamed tool cycle produces validated citations, persistent history and explicit no-answer behavior; same accepted request ID does not duplicate a user message.

### 5 — Lifecycle, budgets and connection

Add manager admission, configuration, credential transport, whole-run budgets, cancellation and startup reconciliation. Test slow headers/body, broken SSE, malformed tool arguments, 401, 429, disconnect, infinite JS execution, oversize outputs and database failure. Gate: bounded cleanup, no runaway request after Stop, one terminal event, no secrets in records/logs, and Retry does not silently duplicate the previous run.

### 6 — Native Ask UI

Integrate panel, scope, history, connection settings, streaming state and internal citations. Wire Close before vault store shutdown. Capture ordered screenshots of Send -> searching -> reading -> streaming -> complete -> source opened; also Stop, error/retry, unconfigured and narrow layout. Inspect real captures. Gate: editor interaction remains responsive and old-run events cannot overwrite a new chat.

### 7 — Retrieval evaluation and footprint

Run a fixed evaluation manifest and store per-question evidence traces. Use deterministic fixture tests for release correctness; use a separately authorized live model evaluation for retrieval quality. Measure cold launch without Ask, first Ask runtime creation, idle runtime disposal, active peak RSS, binary delta and repeated-run memory after forced GC where appropriate. Gate: all deterministic invariants pass; live evaluation reports measured quality and failures without substituting a fabricated percentage; no unexplained sustained memory growth after repeated runs.

### 8 — Packaging and release readiness

Add CI for bundle reproducibility, cgo-free backend tests and cross-compilation, required native shell tests and screenshots on a supported runner, and notices from MIT Pi/Accanto plus Apache-2.0 moejs and its bundled third-party data. Record exact bundle/source/package SHAs. Gate: normal Go builds consume the committed bundle, disabled/unconfigured Ask does no inference/network, and removing Ask leaves synced vault format untouched. Publication/merge occurs only if separately requested.

## 13. Validation commands and fixture matrix

Follow existing build/QA instructions when implementation begins; these are proposed new scripts where absent:

- agent: npm ci; npm run build; npm test; npm run check:bundle.
- root: CGO_ENABLED=0 go test ./internal/assistant ./internal/store ./service ./internal/clipper, then repository-required root suite.
- concurrency: go test -race ./internal/assistant with a race-capable toolchain (race builds are separate from CGO_ENABLED=0 verification).
- shell: prepare the existing patched MyGO dependency with the repository's script, then run required shell tests and native captures. Do not claim root tests validate the nested shell module.
- headless integration: a Go test boots the local model HTTP fixture, a temporary note DB and temporary assistant DB, then runs the embedded agent. No real provider or personal vault access in CI.
- portability: cgo-free build/cross-compile targets from existing project scripts; run at least one real supported desktop build for UI evidence. Cross-compilation alone is not runtime validation.

Required observable regressions:

- Multi-step search/read, empty results and correct no-answer.
- Scope intersected with tags/notebook descendants; IDs guessed by the model denied.
- Malicious note text cannot enable mutations or widen scope.
- Read beyond the first excerpt and valid Unicode boundaries.
- Current note edited/moved/deleted mid-run; stale citation disclosed.
- Unknown citation token never resolves to a note.
- 401/429/provider partial stream and Stop/retry.
- Transaction callback rejection, queued unrelated work and rollback/close correctness.
- Cancelled late events, duplicate request ID and startup interrupted state.
- Runtime allocation exhaustion and long-loop interruption, followed by a healthy new run.
- Chat deletion and fresh runtime reopen; no secret persisted.
- No Joplin note or sync metadata mutation caused by Ask. Assert against relevant fixture tables, not changing timestamps of assistant files.

## 14. Later extensions

After v1: add native Anthropic/Gemini/Responses adapters incrementally using the already embedded Pi architecture; add read-only resource extraction with explicit source types and limits; evaluate embeddings only when the fixed question corpus shows lexical retrieval failures; add optional public-web tools with separate disclosure and network policy.

Code Mode, if added, gets a separate moejs runtime and capability broker. Model-generated code never receives provider fetch, credentials, assistant SQL or the trusted plugin global. Reuse the same note broker and narrower budgets. Adapt Pi's sandbox interface only after inspecting its actual installed contract. Test infinite loops, allocations, read denial, cancellation and disposal independently from the trusted orchestration plugin.

A future shared Accanto agent package should follow demonstrated shared behavior and preserve Accanto browser fixture coverage. Do not generalize the host bridge into a broad plugin platform before both consumers actually need it.

## 15. Completion evidence and handoff

Deliver implementation commits, exact reviewed/tested SHAs, bundle manifest, dependency notices, deterministic test output, native screenshot sequence, footprint table and retrieval evaluation report. Clearly separate fixtures from live-model results. List any omitted platform validation and a concrete follow-up.

Current evidence only: prior local Joplin store/service/clipper tests and Accanto build/research fixtures passed. This plan's moejs bundle, host bridge, SQLite integration and UI have not been implemented or run. The detailed contracts above are proposed application interfaces; the named moejs eventloop/webapi and Pi SqliteStorage APIs were checked against their current documentation/types.

Source anchors:
- https://github.com/MoeForge/moejs/blob/934171643e14a785a9c03e8bee10f82e5e1de478/docs/guide.md
- https://github.com/lsj5031/pi-accanto/blob/c5f7b1a6fc4281201b5f9eb293bf00281f48ab7a/panel.js
- https://github.com/lsj5031/pi-accanto/blob/c5f7b1a6fc4281201b5f9eb293bf00281f48ab7a/attachments.js
- https://github.com/lsj5031/joplin-lite/blob/6472c150f25b912570fad0b51ebac71ca6c76be5/shell/native.go
