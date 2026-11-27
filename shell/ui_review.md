# UI Review: Feedback Check and Improvement Plan

> Scope update (2026-11-27): the browser preview described below was removed
> from the repository. Findings that cite `web/` are historical; the native
> findings and recommendations still apply.

This review checks an external UI critique against the code and the design
specification (DESIGN.md), then lists similar issues found along the way.
Each finding names the file that supports it.

Limits of this review:

- Some quoted note titles in the critique do not appear in the repository,
  so those specific screens could not be reproduced. If they came from your
  own library, a screenshot or the note text is needed to trace them.
- This review read source and specifications only. Nothing was rendered or
  run, so the layout points below are inferred from code and need a visual
  check.

## Summary

The direction of the critique is mostly right, with three corrections:

1. The "row height about 40px" claim does not match the code. Native rows
   have a 36px minimum. Web rows have an 84px minimum. The native list is
   deliberately compact (title only) under DESIGN.md.
2. The secondary-text problem is mainly font size, not colour contrast.
   Secondary ink passes WCAG AA on the paper colour.
3. Save feedback already exists as text. The real missing piece is search
   match highlighting.

The design spec ("No idle animation") and the Ask UI review (no animation on
frequent actions) favour text and colour over motion for status feedback.

## Feedback Points

| # | Critique | Verdict | Evidence |
| --- | --- | --- | --- |
| 1 | Table content is clipped on the right | Partly confirmed (web). Native not verified. | See 1.1 |
| 2 | Secondary text is too faint | Confirmed, but the cause is size | See 1.2 |
| 3 | List titles are truncated | Confirmed (web) | `web/src/components/NoteList.tsx` lines 113 and 117 |
| 4 | List density is too high | Disputed. The stated row height is wrong. | `shell/design.go` line 174; `web/src/styles/app.css` line 146 |
| 5 | No status feedback | Partly confirmed | See 1.5 |
| 6 | Chinese typography is uneven | Confirmed, with specific causes | See 1.6 |

### 1.1 Table overflow

Correction: this section's first version was wrong. After checking the MyGO
renderer, the native table is not clipped; it scrolls sideways. The fix plan
is in `docs/ui-review-plan.md` (N1 and W1).

- Native: MyGO wraps tables in `ScrollHorizontal` (`ui/markdown/table.go`) and
  caps a column at `TableMaxColumn = 24em`, wrapping beyond that. The shell
  never overrides that default, so at the 17px body size a column may reach
  about 408 DIP while the editor measure is 700 DIP: a two-column table
  already overflows and scrolls. Nothing is lost, but macOS hides overlay
  scrollbars, so it looks cut off.
- Web: `.prose-note table` (`web/src/styles/app.css`, lines 299 to 310) has no
  scroll container of its own and its cells have no `overflow-wrap`, so a long
  URL widens the table past the prose column and drags the whole editor
  sideways, taking the header row with it.

### 1.2 Faint secondary text

- Colour: `--color-ink-soft #606F5A` (`web/src/styles/app.css`, line 16) is
  about 5.1:1 on the paper colour `#F9FAF5`. On the sage sidebar `#E9EEE3` it
  is about 4.5:1, which is at the AA minimum.
- Size is the main problem:
  - Web note rows use 14px titles, 12px excerpts, 11.5px metadata and 10px
    times (`app.css` line 148; `NoteList.tsx` lines 113, 117, 119).
  - Native labels are 11 and mono 10 (`shell/design.go` lines 130 to 133).
  - DESIGN.md sets secondary labels to 11 to 12 DIP, so the lower bound of the
    spec is itself small.

### 1.3 Truncated list titles

- Web: both the title and the excerpt use `truncate` (`NoteList.tsx` lines
  113 and 117). The title element has no `title` attribute, so the full text
  cannot be read on hover.
- Native: whether the note row title (`shell/workspace.go`, from line 1295)
  truncates was not checked.

### 1.4 Density

- Native: rows use `MinHeight(rowHeight)` with `rowHeight = 36`
  (`shell/design.go` line 174; `shell/workspace.go` line 1204).
- Web: `.note-row { min-height: 84px }` (`app.css` line 146), and 108px on
  narrow windows (line 181).
- DESIGN.md: "Native note rows are compact and title-only ... the browser
  retains taller excerpt rows." The native compactness is a stated
  requirement.
- The web rows are crowded (title, excerpt, and a three-part metadata line).
  Adjusting them is reasonable, and that change can be limited to the web.

### 1.5 Status feedback

- Present: the native footer shows "Saving…" and "Saved HH:MM:SS"
  (`shell/native.go` lines 2099 to 2110). The web footer shows "Saving
  locally…" and "Saved locally" (`web/src/components/StatusStrip.tsx`).
- Missing:
  - Search match highlighting. Neither the web nor the native code has any
    highlight (`<mark>` or equivalent) for matches. This is the real gap.
  - A visible confirmation for creating a note. No code was found for it.
- Animated save indicators would conflict with DESIGN.md. Use text and a
  dot instead.

### 1.6 Chinese typography

Each point has a traceable cause:

- `<html lang="en">` (`web/src/routes/__root.tsx` line 49). With English
  declared, browsers pick Chinese glyph variants by English rules, which can
  make weights look uneven.
- The web font stack (`app.css` lines 4 to 7) has no Chinese family.
- The native bundled fonts, Overpass and Overpass Mono, contain Latin
  characters only (`shell/fonts/`). Chinese characters fall back to a system
  font, whose weight and metrics differ from the Latin text.
- `.prose-note { max-width: 72ch }` (`app.css` line 189). The `ch` unit is
  based on the width of "0". A Chinese character is roughly 2ch wide, so a
  line holds only about half as many Chinese characters as English ones. The
  design calls for a 700 CSS px measure.
- Table cells (`app.css` lines 305 to 310) have no width rules, so column
  widths for Chinese text depend on content.
- There is no spacing rule between Chinese and Latin text.

## Similar Issues Found

1. Table overflow without a visible cue (see 1.1). Cells need
   `overflow-wrap`, and the table needs a width limit or a scroll container.
2. Too many font sizes on the web note row: 14, 12, 11.5, 11 and 10px
   (`app.css` line 148; `NoteList.tsx` lines 113 to 119). Three levels would
   be enough: title 14px, excerpt and metadata 12px, and count and time in
   11px mono.
3. Conflicting title size rules. `NoteList.tsx` line 49 sets the list heading
   to `text-[18px]`, while `app.css` line 144 sets
   `.note-list > header h2` to 14px. Which one applies depends on the CSS
   cascade. One definition should remain.
4. Native Ask answers are small. The Ask Markdown body uses `sizeUI` (13)
   with a 1.5 line height (`shell/ask_markdown.go` lines 27 and 28), well
   below the 17 used in the editor. Long answers are tiring to read.
5. No way to see a full title. Neither platform offers a hover or expanded
   view of a truncated title.

## Recommended Changes

### P1: reading problems, small changes

- Tables: wrap each table in a horizontally scrolling container and add
  `overflow-wrap: anywhere` to cells. Do this in the Markdown rendering step
  rather than setting `display: block` on the table, which would break table
  layout.
- Chinese font: add `'PingFang SC', 'Microsoft YaHei'` to `--font-sans` so
  Chinese glyphs come from a known face.
- Measure: change `.prose-note` from `max-width: 72ch` to `700px`, matching
  DESIGN.md and the editor.
- Language: set `lang="zh-CN"` when the content is Chinese. The simplest
  approach is to base it on the user's setting, not on per-note detection.

### P2: information and hierarchy

- Web list: add a `title` attribute to the title element. Consider allowing
  two lines (`line-clamp: 2`) on web rows only.
- Reduce font sizes to three levels (see similar issue 2), keep secondary
  text at 12px or larger, and check contrast on the sage background.
- Remove the duplicate heading size rule (similar issue 3).
- Set the Ask answer body to the editor size, or at least 14 to 15px.

### P3: status and feedback

- Search highlighting: on the web, wrap matches in `<mark>`. On native,
  highlight matches with the violet selection colour. First check in
  `tmp/mygo-patched-v0.9.0/docs/ui/` whether the MyGO text component supports
  styled runs.
- New note: move focus to the title after creation and show a one-line
  "New note created" message in the footer. No animation.
- Row height: keep the native compact design, as DESIGN.md requires, and only
  adjust web rows.

## How to Verify

- Web: run the dev server, open a note with Chinese text, a long URL and a
  wide table at 1024px and 1440px. Check the table, glyph weight and line
  length.
- Native: add a Chinese note with a wide table and a long title to the
  screenshot tests (`shell/screens_test.go`), generate the PNGs and inspect
  them. The screenshots use synthetic data, so they show layout only, not
  live rendering on macOS or Windows.
- None of these checks were run for this review. CI or a local run is needed.
