package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// The reference dialog is the one place that lists every key the workspace
// handles. Keep the groups in step with the bindings in view and with the
// platform labels the controls and menus advertise.

type shortcutEntry struct {
	action string
	keys   string
}

type shortcutGroup struct {
	title   string
	entries []shortcutEntry
}

func isDarwin() bool { return runtime.GOOS == "darwin" }

// shortcutKey writes a Command chord on macOS and a Control chord elsewhere,
// as the platform's menus write it.
func shortcutKey(key string) string {
	if isDarwin() {
		return "⌘" + key
	}
	return "Ctrl+" + key
}

// shortcutShiftKey writes Shift+Command on macOS and Ctrl+Shift elsewhere.
func shortcutShiftKey(key string) string {
	if isDarwin() {
		return "⇧⌘" + key
	}
	return "Ctrl+Shift+" + key
}

func shortcutDeleteKey() string {
	if isDarwin() {
		return "⇧⌘⌫"
	}
	return "Ctrl+Shift+Backspace"
}

func shortcutHistoryKeys(forward bool) string {
	if isDarwin() {
		if forward {
			return "⌘]"
		}
		return "⌘["
	}
	if forward {
		return "Alt+→"
	}
	return "Alt+←"
}

func shortcutRenameKey() string {
	if isDarwin() {
		return "F2 or ↩"
	}
	return "F2"
}

// shortcutGroups is the keyboard map in reading order: writing a note, moving
// around, shaping the workspace, and help itself.
func shortcutGroups() []shortcutGroup {
	return []shortcutGroup{
		{title: "Notes", entries: []shortcutEntry{
			{"New note", shortcutKey("N")},
			{"Save now", shortcutKey("S")},
			{"Edit / preview", shortcutKey("E")},
			{"Mark as to-do", shortcutShiftKey("D")},
			{"Delete note", shortcutDeleteKey()},
		}},
		{title: "Find and navigate", entries: []shortcutEntry{
			{"Search notes", shortcutKey("K") + " or " + shortcutKey("F")},
			{"Command palette", shortcutKey("P")},
			{"Back", shortcutHistoryKeys(false)},
			{"Forward", shortcutHistoryKeys(true)},
			{"Note list navigation", "↑ / ↓"},
		}},
		{title: "Workspace", entries: []shortcutEntry{
			{"New notebook", shortcutShiftKey("N")},
			{"New tag", shortcutShiftKey("T")},
			{"Notebooks sidebar", shortcutKey("\\")},
			{"Rename notebook or tag", shortcutRenameKey()},
			{"Ask across notes", shortcutShiftKey("A")},
			{"Sync now", shortcutKey("R")},
			{"Settings", shortcutKey(",")},
		}},
		{title: "Help", entries: []shortcutEntry{
			{"Keyboard shortcuts", "? or " + shortcutKey("/") + " or F1"},
		}},
	}
}

// openShortcuts shows the keyboard reference.
func (a *nativeApp) openShortcuts() {
	a.shortcutsOpen = true
}

// requestDelete starts the confirmation for the multi-note selection when
// there is one, else for the open note.
func (a *nativeApp) requestDelete() {
	if a.selection.Len() > 1 {
		a.confirmBulkDelete = true
		return
	}
	if a.note != nil {
		a.confirmDelete = true
	}
}

// shortcutsDialog is the reference modal. It scrolls on short windows, names
// the current platform's chords, and leaves Escape to the modal's own close.
func (a *nativeApp) shortcutsDialog(c *ui.Context, t *ui.Theme) {
	if !a.shortcutsOpen {
		return
	}
	width, height := c.Size()
	panelW := min(float32(560), max(float32(320), width-96))
	listH := min(float32(360), max(float32(160), height-320))
	groups := shortcutGroups()
	ui.Modal(c, &a.shortcutsOpen, func() {
		ui.Column(c).Width(panelW).Gap(12).Children(func() {
			ui.Text(c, "Keyboard shortcuts").FontSize(scaledText(c, 19)).Bold().
				Role(ui.RoleHeading).Level(2)
			ui.Text(c, "Keys for this computer. Press Esc to close.").
				FontSize(scaledText(c, 12)).TextColor(t.TextMuted)
			ui.Scroll(c.Key("shortcuts-list")).Height(listH).Shrink(0).Children(func() {
				if panelW < 460 {
					shortcutGroupStack(c, t, groups, 14)
					return
				}
				// Two columns keep every group in view at the compact
				// window, where one column would need a long scroll.
				ui.Row(c).AlignItems(ui.Start).Gap(24).PaddingEnd(14).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						shortcutGroupStack(c, t, groups[:min(2, len(groups))], 14)
					})
					if len(groups) > 2 {
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							shortcutGroupStack(c, t, groups[2:], 14)
						})
					}
				})
			})
			ui.Text(c, "Delete always asks first. While you are typing, the editor keeps its own delete keys.").
				FontSize(scaledText(c, 11)).TextColor(t.TextMuted)
			ui.Row(c).Justify(ui.End).Children(func() {
				if primaryAction(c, "Done").AutoFocus().Clicked() {
					a.shortcutsOpen = false
				}
			})
		})
	})
}

// shortcutGroupStack lays groups one below the other, the one-column form
// and the content of each column in the two-column form.
func shortcutGroupStack(c *ui.Context, t *ui.Theme, groups []shortcutGroup, gap float32) {
	ui.Column(c).Gap(gap).Children(func() {
		for _, group := range groups {
			ui.Column(c).Key("shortcuts:" + group.title).Gap(2).Children(func() {
				ui.Text(c, group.title).FontSize(scaledText(c, 11)).Bold().
					TextColor(t.TextMuted).Role(ui.RoleHeading).Level(3)
				for _, entry := range group.entries {
					shortcutRow(c, entry)
				}
			})
		}
	})
}

// shortcutRow is one action and the platform's chord, the chord in a quiet
// keycap at the end of the row.
func shortcutRow(c *ui.Context, entry shortcutEntry) {
	ui.Row(c).MinHeight(26).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, entry.action).FontSize(sizeUI).TextColor(inkNavy).
			Grow(1).MinWidth(0).SingleLine()
		ui.Row(c).Shrink(0).Radius(radiusSmall).Border(1, hairlineStrong).
			Padding(1, 7).Children(func() {
			ui.Text(c, entry.keys).Font(fontMono).FontFeatures("tnum").
				FontSize(sizeMonoSm).TextColor(inkSoft)
		})
	})
}
