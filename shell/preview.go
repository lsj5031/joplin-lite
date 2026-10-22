package main

import (
	"log/slog"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// previewContent only builds visible chunks of long notes. ListState measures
// their heights and anchors scrolling while previously unseen chunks load.
func (a *nativeApp) previewContent(c *ui.Context) {
	start := time.Now()
	defer func() {
		a.metrics.observeAttrs(spanUIMarkdownPreview, start, slog.Int("body_bytes", len(a.body)))
	}()
	body := a.prepareMarkdownOptions()
	if len(a.previewChunks) > 0 {
		ui.List(c.Key("preview-pages"), &a.previewList, len(a.previewChunks), func(row int) {
			ui.Row(c).FillWidth().Children(func() {
				ui.Column(c).Grow(1).MaxWidth(700).Children(func() {
					markdown.View(c, a.previewChunks[row], a.md)
				})
			})
		}).Fill().Gap(0).Padding(20, 48, 48, 48).Label("Note preview")
		return
	}
	ui.Scroll(c).Fill().Padding(20, 48, 48, 48).Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			ui.Column(c).Grow(1).MaxWidth(700).Gap(20).Children(func() {
				if body == "" {
					ui.Text(c, "This note is empty.").FontSize(sizeBody).TextColor(inkSoft)
					return
				}
				markdown.View(c, body, a.md)
			})
		})
	})
}
