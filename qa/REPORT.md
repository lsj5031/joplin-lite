# joplin-lite — feature QA report

_Generated 2026-10-18; Joplin Server gate closed 2026-10-19. Human-readable companion to the canonical spreadsheet
`qa/features.csv` (200 user stories) and the live dashboard at
<http://127.0.0.1:8099/dashboard.html>._

---

## 1. What this is

Every user-visible and API-visible feature of joplin-lite was enumerated from the
source into a single spreadsheet of **200 user stories** with *expected
behaviour*, then each story was exercised against the **production build**
(the pre-rendered SPA in `web/dist/client` + the cgo-free Go binary), and every
failure was fixed and re-tested.

| Phase | What happened | Artifact |
|-------|---------------|----------|
| 1 | Enumerate every feature from code into user stories | `qa/features.csv` (200 rows) |
| 2 | Test every story, log every error | `qa/test_ui.mjs`, `qa/test_api.py`, `qa/test_cli.py`, `qa/collect_go.py`, `shell/backend_test.go` |
| 3 | Fix every defect found | see §4 |
| 4 | Re-test after fixes | all suites green |
| 5 | Record showcase demo clips | `qa/video/*.mp4` |
| 6 | Report + polished product video | this file, `qa/video/joplin-lite-demo.mp4` |

Progress is streamed to `qa/activity.jsonl` and rendered live on the dashboard.

---

## 2. Result at a glance

| Layer | Stories | Pass | Blocked | Fail |
|-------|--------:|-----:|--------:|-----:|
| Workspace UI (`ui`) | 62 | **62** | 0 | 0 |
| Data API (`api`) | 53 | **53** | 0 | 0 |
| Sync engine (`sync`) | 24 | **23** | 1 | 0 |
| Storage & format (`store`) | 13 | **13** | 0 | 0 |
| Service seam (`service`) | 5 | 5 | 0 | 0 |
| CLI & config (`cli`) | 22 | 21 | 1 | 0 |
| Desktop shell (`shell`) | 16 | **16** | 0 | 0 |
| Observability & extras (`cli`) | 5 | 5 | 0 | 0 |
| **Total** | **200** | **198** | **2** | **0** |

* **198 verified passes** — 190 carry an automated test or suite; the seven
  desktop-shell stories (`SH-01`, `SH-06`, `SH-07`, `SH-09`, `SH-10`, `SH-11`,
  `SH-12`) were verified on a real macOS session against the built binary, and
  `SYNC-23` was verified against a real Joplin Server 3.x in Docker; all record
  their evidence (`qa/collect_go.py` → `MANUAL`).
* **2 blocked** — not failures: `SYNC-24` needs a live S3 bucket, and `CLI-13`
  needs an interactive Dropbox OAuth redirect. Each has an explicit reason in
  the spreadsheet and §5.
* **0 failing** stories remain after the fixes in §4.

Raw evidence: `qa/results-ui.json`, `qa/results-api.json`, `qa/results-go.json`,
`qa/results-cli.json`, merged into `qa/results-all.json`. The Joplin Server pass
is reproducible with `qa/joplin-server-e2e.sh` against a Docker server.

`go test ./...` passes across all modules (plus `shell/`, a separate module),
`go vet` is clean, and the previously flaky local-folder rehearsal now passes
**40/40** runs (§4.12).

### Coverage closed in this pass

The first enumeration missed an advertised-but-unbuilt surface. It is now
enumerated and either fixed or honestly flagged:

* **Observability (tracing/metrics)** — `config.yaml.example` shipped with both
  **enabled**, and `DefaultObservability()` defaulted them on, while nothing
  exported spans and no `/metrics` endpoint existed. That is a silent no-op.
* **RAG** and **`mcp.enabled_tools`** — documented as working in the example
  config; neither has a runtime implementation.
* **`PUT /revisions/{id}`** — implemented (refuses rewrites) but untested and
  undocumented.

---

## 3. How to reproduce

```bash
# 1. Build the production assets + both binaries.
bash shell/build.sh         # web/dist/client -> shell/webdist -> tmp/gojoplin-desktop.exe
bash qa/reset.sh            # Go clipper binary + backends 41888–41893

# 2. Run the suites.
node qa/test_ui.mjs         # 62 UI stories   -> qa/results-ui.json
python3 qa/test_api.py      # 52 API stories  -> qa/results-api.json
python3 qa/test_cli.py      # 22 CLI/OBS      -> qa/results-cli.json
python3 qa/collect_go.py    # Go + shell unit/integration -> qa/results-go.json

# 3. Fold results into the canonical spreadsheet + dashboard feed.
python3 qa/merge_results.py
python3 qa/track.py import qa/results-all.json
python3 qa/track.py summary

# 4. Watch it live.
open http://127.0.0.1:8099/dashboard.html

# 5. Re-record the demo video (optional).
bash qa/reset.sh && node qa/make_demo.mjs

# Stop the QA backends.
bash qa/servers.sh stop
```

`qa/features.csv` is the single source of truth; `qa/track.py` is the only writer.
The QA rig never touches the user's live Joplin vault — it uses scratch data
directories under `qa/run/`.

---

## 4. Defects found and fixed

### UI

1. **Conflicting notes were invisible to search and scoped lists** — `UI-23`
   _(high)_. `internal/clipper/search.go`, `internal/clipper/tags.go`
   (`include_conflicts` honoured) + `web/src/lib/api.ts`.

2. **Sidebar notebook/tag counts disappeared** — `UI-33` _(medium)_.
   `web/src/lib/vault.tsx` flips `countsExact` on once the library is fully loaded.

3. **Dead conflict-banner buttons** — `UI-22` _(high)_. Buttons are now
   **Open synced version** / **Keep my copy**; `web/src/components/EditorPane.tsx`,
   `web/src/lib/{api.ts,mock-data.ts}`, and a `pinned` selection ref in
   `web/src/routes/index.tsx` so an out-of-list note stays selected.

4. **React hydration error #418 on first load** — `UI-62` _(medium)_.
   `web/src/client.tsx` (custom `hydrateRoot`) + `data-theme`/`suppressHydrationWarning`
   in `web/src/routes/__root.tsx`.

### Data API

5. **`body_html` ignored on create** — `API-22` _(medium)_.
6. **Create response omitted `type_`** — `API-20` _(low)_.
7. **Summary excerpts were 141 runes** — `API-16` _(low)_; capped to 140.
8. **`GET /notes/{id}/tags` ignored pagination** — `API-34` _(medium)_.
9. **Resources bypassed the write policy** — `API-43` _(high)_.
10. **Orphan notes accepted a nonexistent notebook** — `API-50` _(medium)_.
11. **Resource title clobbered by an empty form field** — `API-43` area _(low)_.
12. **`PUT /revisions/{id}` was untested** — `API-53` _(low)_. History is
    immutable; a rewrite is refused (400) after confirming the id exists, and an
    unknown id is 404. Now pinned by `TestClipperRevisionRewriteRefused`.

### Sync engine

13. **A rare same-millisecond edit could be silently dropped on sync** —
    `SYNC-17` _(high)_. `ApplyPulledItem` now records the **pulled revision's
    `updated_time`** as the sync point (`internal/store/sync_state.go`,
    `internal/store/pulled.go`; new `UpsertSyncItemAt`). This also explains and
    eliminates the flaky integration test — see §6.

### Observability / docs (silent no-ops)

14. **Tracing and metrics were on by default but did nothing** — `OBS-01`,
    `OBS-02` _(medium)_. `DefaultObservability()` now defaults them **off**, and
    `Config.UnsupportedSubsystems()` reports settings that are enabled but have
    no runtime implementation. `cmd/serve.go` and `shell/backend.go` log a
    `"configured but not implemented"` warning instead of silently ignoring them.
15. **RAG was documented but unimplemented** — `OBS-03` _(medium)_. Same
    treatment: parsed, defaults off, warns when on, search stays FTS.
16. **`mcp.enabled_tools` was parsed but never enforced** — `OBS-04` _(low)_.
    A non-`*` value is now reported as not implemented.
17. **The example config advertised unbuilt features** — `OBS-05` _(medium)_.
    `config.yaml.example` no longer enables tracing/metrics by default, annotates
    the unimplemented subsystems, and drops the "13 MCP tools" claim.

### Desktop shell (verification, not a bug)

18. **The in-process backend had no headless test** — `SH-04`, `SH-05`,
    `SH-08`, `SH-14`, `SH-15`, `SH-16`. Added `shell/backend_test.go`, which
    starts the real backend, probes `/ping` and an authorized `/notes`, checks the
    ephemeral-vs-configured token behaviour, the address/data-dir overrides, the
    profile key, and idempotent `Close()`. No webview required.

### Follow-up pass (store tests + real desktop session)

19. **Store test gaps closed** — `MOD-08`, `MOD-13`. Added
    `internal/store/settings_test.go` (settings KV round-trip, overwrite, empty
    and unicode values, survives a reopen) and `internal/store/events_test.go`
    (item_changes page strictly after a cursor, `has_more`, no duplicates,
    ordered ids, single-event lookup, resumable after new mutations).
20. **Conflict notes leaked from tag lists and search** — `API-18` _(high)_.
    `GET /tags/{id}/notes` and `GET /search` returned conflict copies by default
    in full-note mode while `/notes` and `/folders/{id}/notes` hid them. Both
    routes now honour `include_conflicts`; `TestNotesListExcludesConflicts` and
    `qa/test_api.py` API-18 cover all four routes in full and summary mode.
21. **The write allow-list could be bypassed by a note move** — `API-49`
    _(high)_. `PUT /notes/{id}` checked the note's **current** folder and then
    accepted any `parent_id`, so an allow-listed note could be moved into a
    read-only notebook or the root. The target folder is now policy-checked;
    API-49 asserts both refusals.
22. **Orphan-parent check ran after the policy check** — `API-50` _(low)_.
    Under an allow-list a nonexistent parent returned 403 instead of the
    documented 400. Parent existence is validated first; API-50 covers the
    allow-list case.
23. **The native window title was the page slug** — `SH-01` _(medium)_. The
    webview adopts `document.title`; `web/src/routes/__root.tsx` now sets
    `Joplin Lite` (was `joplin-lite`). Verified on a real session, along with
    `SH-06` (second launch exits while the first keeps running), `SH-07`
    (window-state save/restore across relaunch), `SH-09` (occupied 41184 → the
    native dialog names the port and the app quits after dismissal), `SH-10`
    (pairing dialog, `/auth/check` waiting, default button rejects) and `SH-12`
    (`-smoke`).
24. **Overlap coalescing had no standalone test** — `SYNC-19`. Added
    `internal/sync/engine_coalesce_test.go`: a pass held inside the backend
    refuses an overlapping `Sync` (try-lock, one lock acquisition), and the
    buffered trigger channel keeps exactly one pending signal.
25. **The macOS app bundle / Dock icon path is now packaged and probed** —
    `SH-11`. Added `shell/bundle.sh` → `tmp/Joplin Lite.app`
    (`com.joplinlite.desktop`, `app-icon.icns`). The runtime path is unchanged:
    the shell reads `android-chrome-512x512.png` from the embedded bundle and
    calls `Dock.SetIcon`, logging any failure. `-smoke` now records the outcome,
    and a real session reports `"icon": "ok"` with no warning.
26. **A fresh Joplin Server target failed on its first sync** — `SYNC-23`
    _(high)_. The server answers GET for a missing `info.json` with 404, and
    `Client.Get` returned that as an error, so `PushSyncInfo` refused before the
    first push. `Client.Get` now returns a typed `ErrNotFound`, and only the
    sync-info path treats it as an empty target (the local, Dropbox and S3
    backends already return `nil, nil` for a missing object); item and blob
    fetches keep failing rather than overwrite local data with empty bytes.
    Covered by `TestClientGet_MissingItemReturnsNotFound` and
    `TestPushSyncInfo_MissingRemoteIsCreated`. Running the new
    `qa/joplin-server-e2e.sh` also exposed that its E2EE leg was only setting a
    password with no master key, so it round-tripped plaintext; the harness now
    seeds vault A with `tools/e2eeseed`, inspects the server-side envelope, and
    checks a third passwordless vault. Both legs pass against Joplin Server
    3.x in Docker (SQLite).

---

## 5. Blocked stories (no failure — needs an environment we don't have)

| Stories | Why blocked |
|---------|-------------|
| `SYNC-24` | Real S3 needs `-tags s3` + AWS credentials; the default stub is what compiles in. |
| `CLI-13` | `dropbox-auth` needs an interactive OAuth redirect; the loopback flow is unit-tested (`TestDropboxOAuth_LoopbackFlow`). |

---

## 6. The flaky sync test (deep dive)

`TestEngineLocalFolderRehearsal` failed about **15%** of runs with
`edit not pushed to target`. Root cause was the `SYNC-17` defect above:

* the rehearsal edits a note immediately after pulling it;
* `ApplyPulledItem` stamped `sync_time = time.Now()`;
* `UpdateNote` also stamps `updated_time = time.Now()`;
* when both landed in the same millisecond, `updated_time > sync_time` was false
  and the push silently skipped the edit.

Recording the **pulled revision's** time as the sync point removes the race
entirely. Verified `0 failures / 40 runs`, plus repeated full-suite passes.

---

## 7. Showcase clips & product video

Five showcase flows recorded from the real production SPA (headless Chrome,
Playwright video capture with captions and a synthetic cursor burned in):

| Clip | Feature shown |
|------|---------------|
| `01-library-and-search.mp4` | Instant full-text search, notebook/tag filters |
| `02-markdown.mp4` | Markdown writing + sanitized live preview |
| `03-organize.mp4` | Inline notebook creation, tag chips |
| `04-conflicts.mp4` | Conflict banner + "Open synced version" / "Keep my copy" |
| `05-todos.mp4` | To-do completion and progress |

**Polished product video:** `qa/video/joplin-lite-demo.mp4` (≈72 s) — intro card,
a titled segment per clip, and a results outro. The clips are generated
locally and are not checked in; re-record them with
`bash qa/reset.sh && node qa/make_demo.mjs`.

---

## 8. Notes & limitations

* Tests run against disposable backends, never the user's vault.
* The desktop binary was rebuilt (`bash shell/build.sh`), so the fixes are now
  embedded in `tmp/gojoplin-desktop.exe`; an already-running instance keeps the
  old build until relaunched.
* `web/src/client.tsx` is a new, currently **untracked** file (the hydration fix);
  it should be `git add`-ed.
* Observability, RAG and `mcp.enabled_tools` are **config-only** for now: they
  parse and validate, but are off by default and warn when enabled. Removing the
  warning means implementing them (OTLP SDK + exporter, a Prometheus endpoint,
  an embeddings client and an MCP tool registry).
* Folder/tag search is **prefix** match by design (`title LIKE q || '%'`).
* Backend waiting semantics for `API-07`–`API-09` are evidenced by the Go test
  `TestClipperAuthHandshake`, because the headless prompt settles instantly.
