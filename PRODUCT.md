# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

TanStack Start (latest) + React + TypeScript — user-directed ("try tanstack.com/start/latest with react"). Prototype UI lives in `web/`. The production shell is MyGO (webview) over a headless Go backend; editor dependencies planned earlier (CodeMirror 6, markdown-it, KaTeX, DOMPurify) still govern note rendering. [inferred from INTENT.md] the prototype's data layer is mock-only in this phase and swaps to MyGO typed bindings later.

## Users

[inferred from INTENT.md] Primary user: the project owner — a developer and long-time Joplin user who keeps one note library shared with Joplin desktop and mobile, synced to their own Dropbox target with E2EE enabled, and clips pages from the browser with the official Web Clipper extension. Situation: wants notes open instantly, without the ~200 MB Electron footprint. Job: capture, read, edit, and sync the same library everywhere with zero data risk.

## Product Purpose

joplin-lite is a small Go desktop client that is a genuine Joplin peer: it reads and writes Joplin's item format losslessly, decrypts and re-encrypts the library end-to-end, syncs through pluggable targets (Dropbox first), and accepts clips from the official Web Clipper. Success = it interchanges with official Joplin clients through a shared sync target with no data loss, in a single small binary that starts in under a second.

## Positioning

A real Joplin peer at a fraction of the footprint: full E2EE and multi-target sync interoperability plus a clipper-compatible server, in one ~7 MB binary — where every other lightweight option is read-only, format-lossy, or a sync-server of its own.

## Operating Context

Runs as a desktop app (MyGO webview shell) beside Joplin desktop and mobile on the same sync target. The clipper extension in the browser talks to a loopback server (default port 41184, ping-scanned 41184–41194). E2EE master password is entered by the user; it never leaves clients. Sync targets are the user's own Dropbox (v1 driver), later local folder / WebDAV / S3 / Joplin Server / OneDrive.

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

Name: **joplin-lite**. No other identity assets exist yet. [inferred] visual identity for the UI is open — this prototype establishes it.

## Evidence on Hand

- `reference/go-joplin` — the AGPL fork: proven `internal/e2ee` port, item serialize/unserialize, SQLite store, sync engine + S3/Joplin-Server drivers, clipper server. Path: `reference/go-joplin/`.
- `INTENT.md` — requirements and success criteria (authoritative for product truth).
- Real encrypted notes on the user's Dropbox sync target exist but are not in this repository. No user-facing UI exists yet; all prototype data is synthetic mock content and must be labeled as such.

## Product Principles

1. Losslessness first — round-trips are byte-identical or the feature does not ship.
2. Never lose data — conflicts become notes, never overwrites.
3. Crypto is a port, not a rewrite — validated against Joplin with interop tests.
4. The shell is only the shell — the backend is headless and independently testable.
5. Footprint is a feature — instant start, small binary, low RAM.
