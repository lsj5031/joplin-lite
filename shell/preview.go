package main

import (
	"log/slog"
	"strings"
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
				ui.Column(c).Grow(1).MaxWidth(contentMeasure).Children(func() {
					var top float32
					if row < len(a.previewChunkTops) {
						top = a.previewChunkTops[row]
					}
					a.previewMarkdown(c, a.previewChunks[row], a.previewChunkStarts[row], top)
				})
			})
		}).Fill().Gap(0).Padding(20, a.contentPadding(), 48, a.contentPadding()).Label("Note preview")
		return
	}
	ui.Scroll(c).Fill().Padding(20, a.contentPadding(), 48, a.contentPadding()).Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			ui.Column(c).Grow(1).MaxWidth(contentMeasure).Gap(20).Children(func() {
				if body == "" {
					ui.Text(c, "This note is empty.").FontSize(sizeBody).TextColor(inkSoft)
					return
				}
				a.previewMarkdown(c, body, 0, 0)
			})
		})
	})
}

func (a *nativeApp) previewMarkdown(c *ui.Context, source string, start int, top float32) {
	a.md.OnTask = nil
	if a.note != nil && a.note.EncryptionApplied == 0 {
		id, raw, preview := a.note.ID, a.previewRaw, a.previewBody
		a.md.OnTask = func(offset int, checked bool) {
			a.setPreviewTask(id, raw, preview, start+offset, checked)
		}
	}
	view := markdown.View(c, source, a.md)
	if top > 0 {
		// A chunk in its own view loses the spacing its first block gets
		// from following the previous chunk's last block.
		view.Margin(top, 0, 0, 0)
	}
}

// The preview only inserts spaces before line endings. Map the clicked marker
// by line and byte column, not by searching task text (which may be duplicated).
// Chunk offsets refer to the complete preview, including off-screen paragraphs.
func previewSourceOffset(raw, preview string, offset int) int {
	if offset < 0 || offset >= len(preview) {
		return -1
	}
	rawStart, previewStart := 0, 0
	for {
		nl := strings.IndexByte(preview[previewStart:offset], '\n')
		if nl < 0 {
			return rawStart + offset - previewStart
		}
		rawNL := strings.IndexByte(raw[rawStart:], '\n')
		if rawNL < 0 {
			return -1
		}
		rawStart += rawNL + 1
		previewStart += nl + 1
	}
}

func (a *nativeApp) setPreviewTask(id, raw, preview string, offset int, checked bool) {
	if a.note == nil || a.note.ID != id || a.body != raw {
		return
	}
	pos := previewSourceOffset(raw, preview, offset)
	if pos < 1 || pos+1 >= len(raw) || raw[pos-1] != '[' || raw[pos+1] != ']' {
		return
	}
	current := raw[pos]
	if current != ' ' && current != 'x' && current != 'X' {
		return
	}
	if (current != ' ') == checked {
		return
	}
	value := " "
	if checked {
		value = "x"
	}
	a.body = raw[:pos] + value + raw[pos+1:]
	a.previewPreserveBody = a.body
	a.markDirty()
}
