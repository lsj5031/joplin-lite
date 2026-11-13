package main

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var windowsChrome = runtime.GOOS == "windows"

func configureWindowsChrome(opts *mygo.WindowOptions) {
	opts.TitleBarStyle = mygo.TitleBarHidden
	opts.TitleBarHeight = 48
	opts.Vibrancy = mygo.VibrancyMica
	opts.AutoHideMenuBar = true
}

// windowsTitleBar follows Kero's integrated, draggable chrome while retaining
// the system caption buttons and the insets reported by MyGO.
func (a *nativeApp) windowsTitleBar(c *ui.Context, t *ui.Theme) float32 {
	bar := c.TitleBar()
	height := max(float32(48), bar.Height)
	ui.Row(c.Key("windows-titlebar")).Height(height).Shrink(0).
		Padding(0, bar.Right+12, 0, bar.Left+12).Gap(8).
		AlignItems(ui.Center).DragWindow().Label("Window toolbar").Children(func() {
		mark(c)
		ui.Text(c, "Joplin Lite").FontSize(scaledText(c, 14)).Bold().TextColor(inkNavy).Shrink(0)
		railLabel := "Open notebooks"
		if a.railOpen && !a.narrow {
			railLabel = "Close notebooks"
		}
		if iconButton(c, "window-notebooks", iconSidebar, railLabel, "Toggle notebooks and tags (Ctrl+\\)").Clicked() {
			a.railOpen = !a.railOpen
		}
		ui.Spacer(c)
		a.searchField(c, t)
		ui.Spacer(c)
		if primaryAction(c, "New note").Clicked() {
			a.newNote()
		}
		if chromeButton(c, "Ask").Tooltip("Ask across your notes (Ctrl+Shift+A)").Clicked() {
			a.toggleAsk()
		}
		if iconButton(c, "window-settings", iconSettings, "Settings", "Settings (Ctrl+,)").Clicked() {
			a.openSettings()
		}
	})
	return height
}
