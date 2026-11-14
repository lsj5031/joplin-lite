package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

func longPreviewFixture() string {
	var b strings.Builder
	for i := range 1400 {
		fmt.Fprintf(&b, "Row %04d 中文 mixed text with punctuation。 readable words and numbers 12345\n", i)
	}
	b.WriteString("Last preview line")
	return b.String()
}

func TestPreviewShortAndInlineBodiesUnsplit(t *testing.T) {
	for i, body := range []string{
		strings.Repeat("a normal source line\n", 200),
		"**" + strings.Repeat("unsplittable text ", 2000) + "end**",
	} {
		app := &nativeApp{body: body, md: &markdown.Options{}}
		app.prepareMarkdownOptions()
		if len(app.previewChunks) != 0 {
			t.Fatalf("fixture %d: a short body or an intact inline span was split", i)
		}
	}
}

func manyParagraphFixture() string {
	var b strings.Builder
	for i := range 600 {
		fmt.Fprintf(&b, "第%03d段：这是一段中文笔记，包含标点和链接。\n\n", i)
	}
	return b.String()
}

func TestPreviewVirtualizedOpeningScrollingAndReplacement(t *testing.T) {
	registerFonts()
	app := &nativeApp{body: longPreviewFixture(), md: &markdown.Options{}}
	tt := ui.NewTester(app.previewContent, 796, 600)
	first, last := app.previewList.Visible()
	if len(app.previewChunks) < 10 || first != 0 || last >= len(app.previewChunks)/2 {
		t.Fatalf("opening built too much: visible=%d..%d chunks=%d", first, last, len(app.previewChunks))
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "Last preview line") {
		t.Fatal("opening eagerly built the document's end")
	}
	app.previewList.ScrollToEnd()
	tt.Frame()
	if _, last := app.previewList.Visible(); last != len(app.previewChunks)-1 {
		t.Fatal("cannot scroll to the end")
	}
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "Last preview line") {
		t.Fatal("the final content is missing")
	}
	tt.SetSize(520, 400)
	tt.Frame()
	app.body = "Replacement start\n" + longPreviewFixture()
	tt.Frame()
	if first, _ := app.previewList.Visible(); first != 0 {
		t.Fatal("replacing the body retained the previous scroll position")
	}
	app.body = "A short note"
	tt.Frame()
	if len(app.previewChunks) != 0 || !tt.HasText("A short note") {
		t.Fatal("short note did not return to a single Markdown view")
	}
	app.body = longPreviewFixture()
	tt.Frame()
	if first, _ := app.previewList.Visible(); first != 0 {
		t.Fatal("returning to a long note did not start at the top")
	}
} // comparePreviewFirstScreen asserts that the virtualized preview shows the
// same first screen as one Markdown view of the whole body.
func comparePreviewFirstScreen(t *testing.T, body string, dark bool, width, height int) {
	t.Helper()
	applyPalette(dark)
	t.Cleanup(func() { applyPalette(false) })

	app := &nativeApp{body: body, md: &markdown.Options{}, dark: dark}
	rendered := app.prepareMarkdownOptions()
	reference := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(designTheme())
		ui.Scroll(c).Fill().Padding(20, 48, 48, 48).Children(func() {
			ui.Row(c).FillWidth().Children(func() {
				ui.Column(c).Grow(1).MaxWidth(700).Children(func() {
					markdown.View(c, rendered, app.md)
				})
			})
		})
	}, width, height)
	actual := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(designTheme())
		app.previewContent(c)
	}, width, height)
	// Scrollbar geometry reflects estimated rather than fully measured content
	// height; compare the reading area, not the scrollbar.
	want, got := reference.Image(), actual.Image()
	for y := 0; y < height; y++ {
		a, z := want.PixOffset(40, y), want.PixOffset(width-40, y)
		if !bytes.Equal(want.Pix[a:z], got.Pix[a:z]) {
			t.Fatalf("reading area differs at y=%d", y)
		}
	}
}

func TestPreviewVirtualizedFirstScreenMatchesWholeDocument(t *testing.T) {
	registerFonts()
	for _, tc := range []struct {
		name   string
		body   func() string
		height int
	}{
		{"one-paragraph", longPreviewFixture, 600},
		// Tall enough to show the seam between the first two chunks.
		{"many-paragraphs", manyParagraphFixture, 1100},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{520, 1000} {
				t.Run(fmt.Sprintf("%s/dark=%v/width=%d", tc.name, dark, width), func(t *testing.T) {
					comparePreviewFirstScreen(t, tc.body(), dark, width, tc.height)
				})
			}
		}
	}
}
