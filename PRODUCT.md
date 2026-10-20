# Product

<!-- impeccable:product-schema 1 -->

## Platform

desktop (native Go UI via MyGO `ui` in `shell/`; optional standalone browser SPA in `web/`)

## Stack

Go (`CGO_ENABLED=0`) + MyGO native `ui` (`shell/`) over the headless Go `service/`, `internal/store/`, `internal/sync/`, and `internal/clipper/` packages. There is no embedded webview, Chromium/WebView2 process, or Node runtime in the desktop binary. An optional React/TanStack browser UI lives in `web/` for standalone browser preview over the HTTP Data API.

## Users

Primary user: the project owner — a developer and long-time Joplin user who keeps one note library shared with Joplin desktop and mobile, synced to their own Dropbox target with E2EE enabled, and clips pages from the browser with the official Web Clipper extension. Situation: wants notes open instantly, without the ~200 MB Electron footprint. Job: capture, read, edit, and sync the same library everywhere with zero data risk.

## Product Purpose

joplin-lite is a small Go desktop client that is a genuine Joplin peer: it reads and writes Joplin's item format losslessly, decrypts and re-encrypts the library end-to-end, syncs through pluggable targets (Dropbox first), and accepts clips from the official Web Clipper. Success = it interchanges with official Joplin clients through a shared sync target with no data loss, in a single small binary that starts in under a second.

## Positioning

A real Joplin peer at a fraction of the footprint: full E2EE and multi-target sync interoperability plus a clipper-compatible server, in one cgo-free single binary — where every other lightweight option is read-only, format-lossy, or a sync-server of its own.

## Operating Context

Runs as a single-process native desktop app (`shell/`, rendered directly by MyGO `ui`) beside Joplin desktop and mobile on the same sync target. The clipper extension in the browser talks to the in-process loopback server (default port 41184, ping-scanned 41184–41194). E2EE master password is entered by the user or read in memory via explicit official Keychain launch opt-in; it never leaves clients. Sync targets are the user's own Dropbox, local folder, Joplin Server, and optional S3.

## Capabilities and Constraints

- Local SQLite store (Joplin's model, schema v49); plaintext at rest by design.
- E2EE is load-bearing and lands first: JED01 envelopes, legacy SJCL (AES-CCM) + native v3.2 methods (KeyV1/StringV1/FileV1, AES-256-GCM). Interop is unforgiving — e.g. KeyV1's decrypted key is used as the 512-char hex string downstream.
- Lossless item round-trip is the product's first principle: unknown properties preserved, re-serialization byte-identical.
- Conflicts become Joplin-style conflict notes; never overwrite. Locks and target version checks protect shared targets.
- One sync engine, drivers behind a small `SyncBackend` interface. Dropbox is the only v1 target ("all of them" is a roadmap, not a milestone).
- Clipper server is compatible with the official Web Clipper (ping / auth handshake / Data API). It cannot coexist with Joplin desktop on port 41184 — surface that clearly.
- v1 is not a plugin host, not mobile, not a sync server.
- AGPL-3.0-or-later (fork of jescarri/go-joplin); a factor if ever distributed.
- Time/encoding rules: `\n`, UTF-8, ISO-8601 UTC in files; unix ms in memory.

## Brand Commitments

Name: **joplin-lite**. Visual direction: **Field Notes — Humanist** (`DESIGN.md`), featuring a sage binding (`#E9EEE3`), warm paper editor (`#F9FAF5`), green-charcoal ink (`#344238`), forest accent (`#566C4A`), Optima headings, Avenir Next reading/UI copy, Overpass Mono machine metadata, and a folded-sheet mark.

## Evidence on Hand

- `shell/` — native desktop workspace, editor/preview, notebook/tag management, sync status footer, Settings dialog, and 36 headless screenshot states (`TestCaptureScreens`).
- `reference/go-joplin` — the AGPL fork: proven `internal/e2ee` port, item serialize/unserialize, SQLite store, sync engine + S3/Joplin-Server drivers, clipper server.
- `INTENT.md` and `DESIGN.md` — requirements, success criteria, and Field Notes visual specification.

## Product Principles

1. Losslessness first — round-trips are byte-identical or the feature does not ship.
2. Never lose data — conflicts become notes, never overwrites.
3. Crypto is a port, not a rewrite — validated against Joplin with interop tests.
4. The shell is only the shell — the backend is headless and independently testable.
5. Footprint is a feature — instant start, small binary, low RAM.
