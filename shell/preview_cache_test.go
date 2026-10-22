package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// Deliberately use one Markdown view, not previewChunks, to measure the
// dependency fix with the application's fonts and theme.
func BenchmarkPreviewSingleViewWarmFrames(b *testing.B) {
	registerFonts()
	bodies := []struct{ name, body string }{
		{"MixedLinks", strings.Repeat("中文 mixed punctuation。 https://example.com/path\n", 1700)},
		{"LongInlineSpan", "**" + strings.Repeat("中文 mixed punctuation。 ", 3500) + "end**"},
	}
	if path := os.Getenv("PREVIEW_BENCH_FILE"); path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			b.Fatal("cannot read local preview benchmark fixture")
		}
		bodies = append(bodies, struct{ name, body string }{"LocalFixture", string(body)})
	}
	for _, fixture := range bodies {
		b.Run(fixture.name, func(b *testing.B) {
			app := &nativeApp{body: fixture.body, md: &markdown.Options{}}
			body := app.prepareMarkdownOptions()
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Scroll(c).Fill().Children(func() {
					ui.Column(c).FillWidth().MaxWidth(700).Children(func() {
						markdown.View(c, body, app.md)
					})
				})
			}, 700, 600)
			tt.Frame()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tt.Frame()
			}
		})
	}
}

var previewColdGeneration uint32

// Each iteration changes the font size slightly to force cold text layouts,
// without changing the private fixture or clearing global font registrations.
func BenchmarkPreviewColdOpen(b *testing.B) {
	registerFonts()
	body := strings.Repeat("中文 mixed punctuation。 https://example.com/path\n", 1700)
	if path := os.Getenv("PREVIEW_BENCH_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal("cannot read local preview benchmark fixture")
		}
		body = string(data)
	}
	for _, virtual := range []bool{false, true} {
		name := "Stacked"
		if virtual {
			name = "Virtualized"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				app := &nativeApp{body: body, md: &markdown.Options{}}
				preview := app.prepareMarkdownOptions()
				if !virtual {
					app.previewChunks = previewChunks(app.previewDoc, preview, 12000)
				}
				previewColdGeneration++
				app.mdTheme.FontSize += float32(previewColdGeneration) / 4096
				ui.NewTester(func(c *ui.Context) {
					if virtual {
						app.previewContent(c)
						return
					}
					ui.Scroll(c).Fill().Padding(20, 48, 48, 48).Children(func() {
						ui.Column(c).FillWidth().MaxWidth(700).Gap(0).Children(func() {
							if len(app.previewChunks) == 0 {
								markdown.View(c, preview, app.md)
							} else {
								for _, chunk := range app.previewChunks {
									markdown.View(c, chunk, app.md)
								}
							}
						})
					})
				}, 796, 600)
			}
		})
	}
}
