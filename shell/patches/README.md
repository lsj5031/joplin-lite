# MyGO long-document patches

The shell pins MyGO v0.5.5 and replaces it with `tmp/mygo-patched`.
`bash shell/prepare-mygo.sh` copies that release from the verified Go module
cache and applies `mygo-large-layouts.patch`, `mygo-inline-transforms.patch`,
and `mygo-list-estimate.patch`.
`shell/build.sh` runs this step
automatically. Go and Git are required, with Bash as for the existing build.
Nothing changes in the shared module cache. Direct shell `go build`, `go test`,
and editor tooling need the preparation step once after checkout.

The preparation is repeatable. An existing copy must have the matching patch
stamp and pass a reverse-patch check. A stale or conflicting copy is never
overwritten: move `tmp/mygo-patched` aside and rerun preparation. Unrelated
local edits are preserved.

## Policy

Upstream rejects every layout estimated above 8 MiB, even though it already
allows a 64 MiB working set of smaller layouts. A large Markdown paragraph can
therefore be shaped on every frame.

The patch leaves the existing cache unchanged and lazily adds a separate
32 MiB allowance for oversized layouts. Large layouts cannot evict ordinary
labels. The additional cache uses frame recency for eviction and expires
unused entries after eight global frames. Font/layout invalidation clears
both caches. Layouts larger than 32 MiB still render, but remain uncached.

The combined estimated ceiling is **96 MiB**, not a process-memory limit.
Caret/selection reservations remain included. No text splitting, shaping,
font fallback, or Markdown semantics change in the cache patch. First-open
shaping of each admitted layout is unchanged.

The second patch hoists the inline-transform tree scan out of the text paint
loops. A link-heavy paragraph otherwise scans all inline nodes for each line
and visible glyph, even when no inline transform is set. The result is reused
only within each background/foreground paint call, never across frames.
It changes no transform, clipping, or ClearType decisions.

The third patch adds optional `ListState.EstimatedRowHeight` in DIPs. The
ordinary default remains unchanged. Lists of tall content can now avoid
building many rows on their first frame before any height is measured.
Measured heights take over, with the existing anchor-based scrolling.

The shell uses that estimate for long-paragraph previews, renders only visible
chunks plus the toolkit's overscan, and reduces chunks from 12,000 to roughly
1,000 runes. Only paragraphs above 12,000 runes enter this path; short
paragraphs and unsplittable inline spans stay intact. Body changes reset the
list. This retains the existing chunking limitations, but no longer shapes
the whole long paragraph before displaying its beginning.

## Validation

```sh
bash shell/prepare-mygo.sh
cd tmp/mygo-patched
CGO_ENABLED=0 go test ./internal/text ./ui ./ui/markdown
CGO_ENABLED=0 go test ./ui/markdown -run '^$' \
  -bench '^BenchmarkLargeParagraphWarmFrames$' -benchmem
```

Tests cover warm-layout reuse, multiple active paragraphs, label protection,
the large-cache budget, least-recent-frame eviction, expiry, release, changed
parameters, cache invalidation, and rejection beyond the hard limit. A
synthetic Markdown test asserts zero new layouts on warm `ui.Tester` frames;
the benchmark reports timings separately, without timing-based assertions.

Windows/amd64 measurements on 2026-10-22 (Ryzen 5 5600X, Go 1.27.1):

| Synthetic link-heavy Markdown | Warm frame | New layouts/frame |
| --- | ---: | ---: |
| Upstream v0.5.5 | 424.7 ms | 2 |
| Large-layout cache only | 234.6 ms | 0 |
| Both patches | 10.2 ms | 0 |

These are local benchmark samples, not timing guarantees. The complete text,
UI, Markdown, and shell test suites passed, as did vet for those packages and
Linux/macOS cross-builds of the changed toolkit packages. No macOS/Linux
runtime or interactive desktop verification was performed.

Release-size checks (`mygo_noinspector`, `-trimpath`, stripped) kept the webview
example unchanged at 9,672,192 bytes. The native counter grew from 9,464,832 to
9,466,368 bytes (+1,536), attributable to the native text-cache/paint changes;
the linker dependency dump shows the existing UI layout path reaches the new
large-layout cache. No new package dependency was added.
With the additional list-estimate patch, the native counter is 9,466,880 bytes
(another 512 bytes for the estimate field/setup change); the webview example
remains 9,672,192 bytes.

The shell also has `BenchmarkPreviewSingleViewWarmFrames`. It deliberately
bypasses application chunking and uses the shell's fonts and Markdown theme.
Set `PREVIEW_BENCH_FILE` to a local Markdown file to add a private fixture;
the benchmark never logs its body and no fixture is committed.

## Upstream status

Reported as [egoist/mygo#628](https://github.com/egoist/mygo/issues/628), with a
synthetic reproduction, measurements, and the two local fix approaches.

Checked on 2026-10-22: v0.6.2 is the latest published release. Its cache policy,
and upstream main at `976f17c9391fb86f9afeba7a2d3b27b54e6d671a`, still reject
entries above 8 MiB. Upgrading alone does not fix this.

`BenchmarkPreviewColdOpen` measures cold layouts by varying font size slightly
between iterations (including across sub-benchmarks). With a local large-note
fixture, five-iteration samples measured 268 ms for the original stacked
12,000-rune chunks and 51 ms for the virtualized first screen. This is a cold
preview benchmark, not a guarantee for full application startup or note I/O.
Tests cover reaching the last page, resizing, body replacement, returning from
a short note, and pixel-equivalent reading areas in light/dark and narrow/wide
views. The scrollbar uses estimated document height until pages are measured.

Once upstream releases include equivalent fixes, remove the replacement and
preparation step together, upgrade the pin, and retain equivalent regression
coverage. The preview also requires the initial row-height API or an equivalent
upstream way to bound first-frame list construction.
