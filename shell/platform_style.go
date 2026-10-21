package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

const windowsChrome = runtime.GOOS == "windows"

// windowsTitleBar follows Kero's integrated, draggable chrome while retaining
// the system caption buttons and the insets reported by MyGO.
func (a *nativeApp) windowsTitleBar(c *ui.Context, t *ui.Theme) float32 {
	bar := c.TitleBar()
	height := max(float32(48), bar.Height)
	ui.Row(c.Key("windows-titlebar")).Height(height).Shrink(0).
		Padding(0, bar.Right+12, 0, bar.Left+12).Gap(10).
		AlignItems(ui.Center).DragWindow().Label("Window toolbar").Children(func() {
		if iconButton(c, "window-notebooks", iconSidebar, "Open notebooks", "Toggle notebooks and tags (Ctrl+\\)").Clicked() {
			a.railOpen = !a.railOpen
		}
		mark(c)
		ui.Text(c, "Joplin Lite").FontSize(14).Bold().TextColor(inkNavy).Shrink(0)
		ui.Spacer(c).Width(12)
		a.searchField(c, t)
		ui.Spacer(c).MinWidth(48)
		if primaryAction(c, "New note").Clicked() {
			a.newNote()
		}
		if iconButton(c, "window-settings", iconSettings, "Settings", "Settings (Ctrl+,)").Clicked() {
			a.openSettings()
		}
	})
	return height
}
