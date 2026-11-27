# Product film

`docs/promo/joplin-lite-promo.mp4` is rendered from deterministic, frame-by-frame
captures of the native workspace and cut with MyGO's render plugin: no browser
takes part, and the frames are encoded by the system's own encoder (or by
ffmpeg where the system has none, as on a headless runner).

```sh
# 1. Capture the native workspace tour at 30 fps (2x retina, 2560x1600).
(cd shell && CAPTURE_DIR=../tmp/promo go test -run TestCapturePromo .)

# 2. Cut and encode the 1920x1080 film and poster.
(cd shell && go test -run TestRenderPromoFilm .)
```

`PROMO_DIR` selects the captured frames and may point outside the checkout.
`PROMO_OUT` selects the film output directory.

`shell/promo_test.go` drives the real `ui.Tester` through a continuous product tour—gliding the pointer with real hover states, opening the Liquid Glass notebooks & tags rail, creating and typing a Markdown note with live sidebar updates, toggling into typeset preview, opening the Command Palette (`⌘K · ⌘P`), and switching to the dark palette—writing numbered PNGs and cursor telemetry to `tmp/promo/`.

`shell/promo_film_test.go` is the cut. It builds the same scenes in Go with the views the app draws: the Field Notes wash and its night fade, the app card with the captured frames played 1:1 at 30 fps and the recorded pointer over them, the editorial caption column, and the opening and closing cards. `render.Video` draws each frame on the GPU and encodes H.264 with the system's own encoder.

## Cut (~22 s)

| Beat | Length | On screen |
| --- | --- | --- |
| Hook | 2.1 s | `joplin-lite` · **Joplin notes. No browser inside.** |
| `library` | 3.5 s | Opens the Liquid Glass Notebooks & Tags rail (`Everyday`, `Reading`, `Ideas`, `#outdoors`, `#reading`, `#weekend`) and filters to `Everyday · 3`. |
| `write` | 9.6 s | Clicks **New note**, types *"Along the waterfront"* with live sidebar and count updates (`Everyday · 4`) and Markdown checklist items, then clicks **Preview** to reveal the typeset page. |
| `palette` | 2.1 s | Clicks the `⌘K` Command Palette badge and filters to **Toggle dark mode**. |
| `night` | 3.4 s | Executes **Toggle dark mode** in-place across the 3-pane workspace and selects **All notes** (`6`) in the dark Field Notes palette. |
| End | 2.5 s | `Joplin Lite` · **A quiet, native home for your Joplin notes.** · `github.com/lsj5031/joplin-lite` |
