# Windows UI Critique & Element Distillation Review (`shell/`)

**Date:** 2026-10-22 (Pass 3 — All Polish Items Resolved)  
**Target:** `shell/workspace.go`, `shell/platform_style.go`, `shell/native.go`, `shell/design.go`, `shell/settings.go`  
**Design Direction:** Field Notes — Humanist (`DESIGN.md`, `.impeccable/briefs/workspace.md`)  
**Health Score:** **40 / 40 (Exemplary)** *(29 / 40 in Pass 1 → 36 / 40 in Pass 2 → 40 / 40 in Pass 3)*

---

## 1. Executive Summary

All structural, geometric, progressive-disclosure, and responsive polish items identified across Passes 1 and 2 have been implemented and verified across the native Windows shell (`shell/`):

- **Editor header & `48px` reading spine (`shell/workspace.go:1299-1520`)**: Consolidated into a single `48px`-padded header row where notebook and tag capsules hug their text (`.Grow(1)` removed), untagged notes show a quiet `+ Tag` trigger, todo notes show an interactive `☐ To-do` / `☑ Done` pill, `✎ Edit` / `👁 Preview` is a labeled button, and rare/destructive actions (`Mark as To-do`, `Attach files…`, `Delete note…`) live inside a `⋯` menu. The Preview container (`preview-box`) now shares the exact `48px` left spine with the header, note title, conflict banner, and raw Markdown editor (`TestNativeEditorHeaderIsOneQuietRow`).
- **Header metadata & singular/plural polish (`shell/workspace.go:1403-1415`)**: Single-word notes format `"1 word"` instead of `"1 words"`, and when the 3-pane rail is open at compact desktop widths (`1024×640`), the header displays the unclipped tabular timestamp (`12:27`) with `.Shrink(0)` so it never truncates to `"12:27..."`.
- **Note list "Timetable Row" (`shell/workspace.go:1095-1192`)**: Note rows include right-aligned tabular `fontMono` timestamps (`12:27` / `02 Jan`), `1px` bottom hairline dividers on unselected rows, interactive `18×18` checkboxes on todo notes, single-line rendering for bodyless notes (no literal `"Empty note"` filler), and a hover-only overlay drag grip (`⣿`) with `12px` left row padding so the grip never touches row text or checkboxes.
- **Integrated Windows titlebar & narrow (`900×640`) responsiveness (`shell/platform_style.go:19-30`, `shell/workspace.go:318, 786-803`)**: `searchField` now uses `.Grow(2).MinWidth(240).MaxWidth(420)` with balanced titlebar spacers so `"Search all notes…"` + `Ctrl+K` remains centered and never clips at `900×640`. In narrow drawer mode (`railOverlay`), the backdrop scrim is offset by `Top(top)` (`48px` on Windows) so `windowsTitleBar` stays undimmed and accessible above the open drawer.
- **Left rail, search bar, Settings, and typography (`shell/workspace.go:207, 247, 513`, `shell/native.go:1792-1870`, `shell/design.go:167-189`)**: Removed the duplicate Windows `"Notebooks"` header row and close button, hid `Conflicts` when `conflictCount == 0`, dropped duplicate `iconTag` icons on `# tag` rows, made clicking an active tag row toggle the filter off, hid the empty `Saved searches` bookmark icon, wired `Ctrl+K` to focus search, added contextual `‹`/`›` history buttons, added the `"Surface wash"` label above `Sage | Linen | Mist | Kero` in Settings, standardized the `"Joplin Lite"` wordmark across all chrome modes, and guarded font registration with `sync.Once` across all platforms.

---

## 2. Verification Matrix (Pass 1 & Pass 2 Recommendations → Final Status)

| Region / Recommendation | Status | Verification Notes |
|---|---|---|
| **Editor Header: Remove `.Grow(1)` on Notebook & Tags pills** | **Done** | Both pills hug their text (`Shrink(1).MinWidth(0)`); untagged notes render a quiet `+ Tag` `chromeButton`. |
| **Editor Header: Consolidate to 1 row at `48px` padding** | **Done** | Single row at `Padding(12, 48, 4, 48)` matches the note title (`Padding(12, 48, 4, 48)`) and raw editor (`Padding(18, 48, 32, 48)`). |
| **Editor Preview: Align `MaxWidth(700)` body to `48px` left spine** | **Done** | Removed `.Justify(ui.Center)` from `preview-box` row (`workspace.go:1300`); pinned by `TestNativeEditorHeaderIsOneQuietRow`. |
| **Editor Header: Label `Edit` / `Preview` & move rare actions to `⋯`** | **Done** | `hdr-mode` shows `✎ Edit` / `👁 Preview`; `Mark as To-do`, `Attach files…`, and `Delete note…` are in `noteActionsMenu`. |
| **Editor Header: Fix `"1 words"` & prevent `"12:10..."` truncation** | **Done** | Uses `"1 word"` singular and `.Shrink(0)` (compact timestamp when `railOpen` is active). |
| **Empty Editor: Remove `"No note selected"` top header** | **Done** | Only the calm centered empty prompt remains on desktop (`17-empty-editor.png`). |
| **Note List: Add right-aligned `fontMono` timestamp & `1px` hairline** | **Done** | `noteTime(n.UpdatedTime)` + bottom `1px` `hairline.Alpha(0.7)` border on unselected rows. |
| **Note List: Interactive todo checkbox & cut `"Empty note"` filler** | **Done** | `ui.CheckboxBase` (`18×18`) toggles todos directly in the list; bodyless notes render as a clean single line. |
| **Note List: Hide `⣿` drag grip until hover with clean left clearance** | **Done** | `grip` is `Absolute().Left(1).Width(10)` with `Padding(0, 10, 0, 12)` on `noteRow`. |
| **Left Rail: Cut duplicate Windows `"Notebooks"` header & close button** | **Done** | Guarded by `if !a.narrow && !windowsChrome`; `window-notebooks` announces `"Open notebooks"` / `"Close notebooks"`. |
| **Left Rail: Hide `Conflicts` at `0` & drop duplicate `iconTag` on `# tag` rows** | **Done** | `Conflicts` only appears when `conflictCount > 0`; tag rows render `# tagname` without a second tag icon. |
| **Search Bar: Prevent `900×640` clipping, hide empty `Saved searches`, fix `Ctrl+K`** | **Done** | `.Grow(2).MinWidth(240).MaxWidth(420)` keeps `"Search all notes…"` + `Ctrl+K` unclipped at `900×640`. |
| **Narrow Drawer: Keep `windowsTitleBar` undimmed above drawer backdrop** | **Done** | `back` in `railOverlay` uses `Top(top)` (`48px` on Windows) so the titlebar stays bright and interactive. |
| **List Header: Add contextual `‹` / `›` history buttons** | **Done** | `historyButtons(c)` appears in the note list header when `len(a.history) > 1`. |
| **Settings Modal: Split `Appearance` / `Synchronization`, label `Surface wash`, hide E2EE when `Off`** | **Done** | `Theme`, `Surface wash`, and `Sync target` are all explicitly labeled; E2EE only appears when a sync target is enabled. |
| **Typography & Brand Wordmark: Consistent `"Joplin Lite"` & `registerFonts()`** | **Done** | Standardized `"Joplin Lite"` across `windowsTitleBar`, `topBar`, and `railPane`; `registerFonts()` runs via `sync.Once` on all platforms. |
