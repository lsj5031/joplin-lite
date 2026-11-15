# MyGO patches

The shell pins MyGO v0.9.1 and replaces it with `tmp/mygo-patched`.
`bash shell/prepare-mygo.sh` copies that release from the verified Go module
cache and applies `mygo-list-estimate.patch` and `mygo-tooltip-modal.patch`.
`shell/build.sh` runs this step before building.

The directory is a derived copy, not a checkout: do not edit it, and do not
point the `replace` directive at a Git clone. To pick up upstream changes, move
the derived copy aside and rerun the script. Unrelated changes fail `git apply`
instead of being silently overwritten.

## Why these two remain

Earlier releases carried fixes this app had previously backported: the
oversized text-layout budget, the hoisted inline-transform scan, and Reduce
Transparency and High Contrast handling in `plugins/glass`. Those are upstream.
The two remaining patches add APIs the app needs and that upstream does not
have.

### `mygo-list-estimate.patch`

Adds `ui.ListState.EstimatedRowHeight`. The virtualized list assumes rows are as
tall as the largest row it has measured. Markdown paragraphs are cut into
1000-rune chunks, and every chunk shares a block with settings rows and to-dos;
without an estimate, one long paragraph reserves its maximum height for every
chunk and the reading pane scrolls over a large blank area. The list clamps the
estimate to measured bounds and falls back to the tallest measured row when it
is unset.

### `mygo-tooltip-modal.patch`

Adds a `tipped` guard so a tooltip cannot remain above an open modal. The app
opens confirmations and the command palette as modals; without the guard the
tooltip for the control that opened them paints over the modal surface.

## Upstreamed in v0.9.1

`markdown.Options.OnTask` is now upstream. v0.9.1 changed it from a byte-offset
callback to a line-based one: the callback receives the task item's line in the
view's source, from 1, and the state asked for. `shell/preview.go` maps that
line back through the virtualized chunk to the original Markdown bytes, so
task checkboxes keep their save semantics. The old task-checkbox patch is
deleted rather than rebased.

## Validation

```sh
bash shell/prepare-mygo.sh
cd shell && CGO_ENABLED=0 go test ./...
CAPTURE_DIR=../tmp/ui-audit CGO_ENABLED=0 go test -run TestCaptureScreens .
RENDER_DIR=../tmp/render CGO_ENABLED=0 go test -run 'TestRenderScreens|TestRenderFilm' .
```

Review the captured screens for clipping, blank rows, and tooltip/modal
overlap, and review the rendered still/film for layout or font drift. The
preview task tests in the shell cover the line-to-source mapping and its save
failure path.

## Upstream status

Checked 2026-11-16 against v0.9.1. `EstimatedRowHeight` and the tooltip modal
guard are still absent upstream; `Options.OnTask` is present. If a later
release adds either remaining API, delete that patch and its entry from
`shell/prepare-mygo.sh`.
