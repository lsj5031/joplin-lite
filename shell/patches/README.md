# MyGO patches

The shell pins MyGO v0.9.0 and replaces it with `tmp/mygo-patched`.
`bash shell/prepare-mygo.sh` copies that release from the verified Go module
cache and applies `mygo-list-estimate.patch`, `mygo-task-checkboxes.patch`, and
`mygo-tooltip-modal.patch`. `shell/build.sh` runs this step before building.

The directory is a derived copy, not a checkout: do not edit it, and do not
point the `replace` directive at a Git clone. To pick up upstream changes, move
the derived copy aside and rerun the script. Unrelated changes fail `git apply`
instead of being silently overwritten.

## Why these three remain

v0.9.0 carries three fixes this app previously backported, so their patches were
dropped:

- the oversized text-layout budget (`maxLargeLayoutBytes`, `roomBesideLive` in
  `internal/text/layout.go`);
- the hoisted inline-transform scan in `ui/paint.go`;
- Reduce Transparency and High Contrast handling in `plugins/glass`.

The remaining patches add APIs the app needs and that upstream does not have.

### `mygo-list-estimate.patch`

Adds `ui.ListState.EstimatedRowHeight`. The virtualized list assumes rows are as
tall as the largest row it has measured. Markdown paragraphs are now cut into
1000-rune chunks, and every chunk shares a block with settings rows and to-dos;
without an estimate, one long paragraph reserves its maximum height for every
chunk and the reading pane scrolls over a large blank area. The list clamps the
estimate to measured bounds and falls back to the tallest measured row when it
is unset.

### `mygo-task-checkboxes.patch`

Adds `markdown.Options.OnTask`. A checked or unchecked GFM task checkbox in the
preview is a real control: the hit target is the box, the row, or the label; the
click is ignored while a save is committing; failures keep the old draft and
report the error. The rebase for v0.9.0 uses the `CheckboxBase` element API and
`strings.Cut`/`IndexByte` for the marker span.

### `mygo-tooltip-modal.patch`

Adds a `tipped` guard so a tooltip cannot remain above an open modal. The app
opens confirmations and the command palette as modals; without the guard the
tooltip for the control that opened them paints over the modal surface.

## Validation

```sh
bash shell/prepare-mygo.sh
cd shell && CGO_ENABLED=0 go test ./...
CAPTURE_DIR=../tmp/ui-audit CGO_ENABLED=0 go test -run TestCaptureScreens .
```

Review the captured screens for clipping, blank rows, and tooltip/modal
overlap. The patch tests in `ui/markdown` cover the task control and its save
failure path.

## Upstream status

Checked 2026-11-14 against v0.9.0. `EstimatedRowHeight` and `Options.OnTask` are
still absent upstream, and `ui/tooltip.go` still lacks the modal guard. If a
later release adds them, delete the patch and its entry from
`shell/prepare-mygo.sh`.
