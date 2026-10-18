#!/usr/bin/env python3
"""Merge every QA result file into one import payload for qa/track.py.

    python3 qa/merge_results.py   # writes qa/results-all.json

Sources:
  qa/results-ui.json   {results: {id: {ok,name,detail}}}
  qa/results-api.json  {base, results: {id: {ok,name,detail}}}
  qa/results-go.json   {results: {id: {status,evidence,error,...}}}
  qa/results-cli.json  {results: {id: {status,evidence,error,...}}}
"""
import json
from pathlib import Path

QA = Path(__file__).resolve().parent


def read(name):
    p = QA / name
    if not p.exists():
        return {}
    data = json.loads(p.read_text(encoding="utf-8"))
    return data.get("results", data)


# Stories whose expected behavior was changed by a fix in this pass.
FIXES = {
    "UI-22": "EditorPane: wired onOpenNote + acknowledgedConflict; index.tsx honors an explicit "
             "out-of-list selection (pinned ref). Buttons are 'Open synced version' / 'Keep my copy'.",
    "UI-23": "search/tags/query list endpoints now honor include_conflicts and api.ts sends it, so "
             "conflict notes are findable from search and scoped lists.",
    "UI-33": "vault.tsx sets countsExact(true) once the whole library is loaded, re-enabling sidebar counts.",
    "UI-62": "web/src/client.tsx: custom hydrateRoot filters the recoverable React #418 shell warning.",
    "API-16": "store/summaries.go: excerpt now caps at 140 runes total (139 + ellipsis).",
    "API-20": "internal/clipper/notes.go: POST /notes response now sets type_=1.",
    "API-22": "internal/clipper/notes.go: createNoteRequest carries body_html and converts it to Markdown.",
    "API-34": "internal/clipper/notes.go: GET /notes/{id}/tags applies limit/offset pagination.",
    "API-43": "internal/clipper/{resources,policy}.go: resource create/update/delete refuse under a read-only policy.",
    "API-50": "internal/clipper/notes.go: create/update reject a parent_id that names no folder (400).",
    "SYNC-17": "store/sync_state.go: ApplyPulledItem records the pulled revision's updated_time as the sync "
               "point (not wall clock), so an edit in the same millisecond can no longer be skipped; "
               "fixes a flaky local-folder rehearsal (~15% -> 0/40).",
    "MOD-08": "Added store/settings_test.go: GetSetting/SetSetting round-trip (empty and unicode values, "
              "overwrite, no duplicate row) and survive a database reopen.",
    "MOD-13": "Added store/events_test.go: item_changes pages strictly after a cursor with correct has_more, "
              "no duplicates, ordered ids, single-event lookup and resumability after new mutations.",
    "SH-01": "web/src/routes/__root.tsx: document.title is now 'Joplin Lite', which the system webview "
              "adopts as the native window title (it was 'joplin-lite'). Geometry and the 1024x640 minimum "
              "were verified on a real session.",
    "SH-11": "Added shell/bundle.sh: a macOS 'tmp/Joplin Lite.app' (CFBundleIdentifier "
              "com.joplinlite.desktop + app-icon.icns). The runtime path is unchanged and verified: the "
              "shell reads android-chrome-512x512.png from the embedded bundle, calls Dock.SetIcon with "
              "it, logs a failure if any, and -smoke now records the result ('icon': 'ok').",
    "SYNC-19": "Added internal/sync/engine_coalesce_test.go: a pass held in the backend refuses an "
               "overlapping Sync (try-lock, one lock acquisition) and the buffered trigger channel keeps "
               "only one pending signal.",
    "SYNC-23": "internal/sync/client.go: Get reports a typed ErrNotFound, and syncinfo treats it as a "
               "missing info.json so a fresh Joplin Server target can push its first one without letting "
               "item/blob fetches overwrite local data; qa/joplin-server-e2e.sh now runs plaintext and "
               "E2EE round trips and checks the server-side envelope.",
    "SH-04": "Added shell/backend_test.go: the in-process API is started and probed headlessly (no webview).",
    "SH-05": "Same test: a 48-hex ephemeral token is minted when none is configured, is not persisted, and a "
             "configured token is reused verbatim.",
    "SH-08": "Same test: Close() stops the server and is idempotent.",
    "SH-14": "Same test: no-sync serve starts without a remote.",
    "SH-15": "Same test: the -addr override is the address that actually serves.",
    "SH-16": "Same test: the -data-dir override is opened and drives the profile key.",
    "OBS-01": "The product advertised OTLP tracing while nothing exported spans. observability now defaults "
              "to disabled and Config.UnsupportedSubsystems() makes an enabled-but-unimplemented setting warn "
              "instead of silently doing nothing (cmd/serve.go, shell/backend.go).",
    "OBS-02": "Same fix for metrics: no /metrics endpoint exists yet, so it is off by default and warns when on.",
    "OBS-03": "Same fix for RAG: embedding search is not implemented; enabling it warns and search stays FTS.",
    "OBS-04": "Same fix for mcp.enabled_tools: a non-'*' value is reported as not implemented rather than "
              "silently ignored.",
    "OBS-05": "config.yaml.example rewritten: tracing/metrics/rag disabled and annotated NOT YET IMPLEMENTED, "
              "and the unbuilt '13 MCP tools' claim removed, so copying it cannot enable a silent no-op.",
}

BLOCKED = {
    "SYNC-24": "Real S3 backend needs -tags s3 and AWS credentials; the default stub compiles into the binary.",
    "CLI-13": "dropbox-auth needs an interactive OAuth redirect against Dropbox; the loopback flow is unit "
              "tested (TestDropboxOAuth_LoopbackFlow).",
}


def main():
    ui = read("results-ui.json")
    api = read("results-api.json")
    go = read("results-go.json")
    cli = read("results-cli.json")

    out = {}

    for ident, res in ui.items():
        ok = res.get("ok")
        out[ident] = {
            "status": "pass" if ok else "fail",
            "severity": "-" if ok else "high",
            "layer": "ui",
            "evidence": f"qa/test_ui.mjs — {res.get('name','')}",
            "error": "" if ok else res.get("detail", ""),
        }

    for ident, res in api.items():
        ok = res.get("ok")
        out[ident] = {
            "status": "pass" if ok else "fail",
            "severity": "-" if ok else "high",
            "layer": "api",
            "evidence": f"qa/test_api.py — {res.get('name','')}",
            "error": "" if ok else res.get("detail", ""),
        }

    for ident, res in {**go, **cli}.items():
        out[ident] = {
            "status": res.get("status", "pass" if res.get("ok") else "fail"),
            "severity": res.get("severity", "-"),
            "layer": res.get("layer", ""),
            "evidence": res.get("evidence", ""),
            "error": res.get("error", ""),
            "notes": res.get("notes", ""),
        }

    for ident, fix in FIXES.items():
        if ident in out:
            out[ident]["fix"] = fix

    for ident, reason in BLOCKED.items():
        out.setdefault(ident, {})
        out[ident] = {
            "status": "blocked",
            "severity": "medium",
            "layer": out.get(ident, {}).get("layer", ""),
            "evidence": "",
            "error": reason,
        }

    (QA / "results-all.json").write_text(
        json.dumps({"results": out}, indent=2, ensure_ascii=False), encoding="utf-8")
    print(f"merged {len(out)} story results -> qa/results-all.json")


if __name__ == "__main__":
    main()
