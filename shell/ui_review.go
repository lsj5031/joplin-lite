package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// uiReviewMarkdown is the UI review, embedded so the Help menu can show it
// without reading the repository. It is an English copy of the review kept
// next to this file, because the bundled fonts have no Chinese glyphs.
//
//go:embed ui_review.md
var uiReviewMarkdown string

// openUIReview shows the UI review in a modal.
func (a *nativeApp) openUIReview() {
	a.uiReviewOpen = true
}

// uiReviewDialog renders the review in a scrolling modal with the same
// Markdown theme as the note preview, so headings, tables and code match the
// rest of the workspace.
func (a *nativeApp) uiReviewDialog(c *ui.Context, t *ui.Theme) {
	if !a.uiReviewOpen {
		return
	}
	width, height := c.Size()
	panelW := min(float32(720), max(float32(320), width-96))
	bodyH := min(float32(560), max(float32(200), height-200))
	ui.Modal(c, &a.uiReviewOpen, func() {
		ui.Column(c).Width(panelW).Gap(12).Children(func() {
			ui.Text(c, "UI review").FontSize(scaledText(c, 19)).Bold().
				Role(ui.RoleHeading).Level(2)
			ui.Scroll(c.Key("ui-review-body")).Height(bodyH).Shrink(0).Children(func() {
				ui.Column(c).Width(panelW - 32).PaddingEnd(14).Children(func() {
					markdown.View(c, uiReviewMarkdown, a.uiReviewOptions())
				})
			})
			ui.Row(c).Justify(ui.End).Children(func() {
				if primaryAction(c, "Done").AutoFocus().Clicked() {
					a.uiReviewOpen = false
				}
			})
		})
	})
}

// uiReviewOptions reuses the preview's Markdown theme. It builds it on first
// use and keeps the link handler off, since the review has no note links.
func (a *nativeApp) uiReviewOptions() *markdown.Options {
	a.prepareMarkdownOptions()
	if a.uiReviewMD == nil {
		a.uiReviewMD = &markdown.Options{}
	}
	a.uiReviewMD.Theme = a.mdTheme
	return a.uiReviewMD
}
