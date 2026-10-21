package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
	"github.com/lsj5031/joplin-lite/service"
)

// Field Notes workspace: sage navigation beside a paper editor, with notebook
// management on demand. Shared typography and palette live in design.go.

const navRowHeight = float32(32)

// boolF is a bool as the 0/1 an animation moves by.
func boolF(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// view builds the workspace.
func (a *nativeApp) view(c *ui.Context) {
	if a.invalidate == nil {
		a.invalidate = c.Services().Invalidate
	}
	// DESIGN.md's palette and type scale, applied to every widget.
	applyPalette(a.dark)
	c.SetTheme(designTheme())
	if a.pendingOpen != "" {
		c.OpenURL(a.pendingOpen)
		a.pendingOpen = ""
	}
	t := c.Theme()

	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.newNote()
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyN) {
		a.openNewNotebook()
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyT) {
		a.openNewTag()
	}
	if c.Shortcut(ui.Cmd, ui.KeyK) || c.Shortcut(ui.Cmd, ui.KeyF) {
		a.focusSearch = true
		a.focusBody = false
	}
	if c.Shortcut(ui.Cmd, ui.KeyP) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyP) {
		a.openPalette()
	}
	if c.Shortcut(ui.Cmd, ui.KeyBackslash) {
		a.railOpen = !a.railOpen
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.openSettings()
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		a.flush()
	}
	if c.Shortcut(ui.Cmd, ui.KeyE) {
		a.toggleEdit()
	}
	if c.Shortcut(0, ui.KeyF2) || (runtime.GOOS == "darwin" && c.Shortcut(0, ui.KeyEnter)) {
		a.renameSelectedRail()
	}
	a.historyShortcuts(c)
	a.tick()
	a.tickSync()

	a.notebookDialogs(c, t)
	a.tagDialogs(c, t)
	a.settingsDialog(c, t)
	a.savedSearchDialog(c, t)
	a.conflictDialog(c, t)
	a.commandPalette(c, t)

	if a.confirmDelete {
		if confirmDialog(c, dangerTheme, &a.confirmDelete, "Delete note?",
			"The note is removed from this device and the deletion is queued for the next sync.",
			"Delete") == 1 {
			a.deleteNote()
		}
	}

	if a.confirmBulkDelete {
		n := a.selection.Len()
		if confirmDialog(c, dangerTheme, &a.confirmBulkDelete, "Delete notes?",
			fmt.Sprintf("The %d selected notes are removed from this device and the deletions are queued for the next sync.", n),
			"Delete notes") == 1 {
			a.deleteSelected()
		}
	}

	width, height := c.Size()
	a.narrow = width < narrowWidth
	if a.railW <= 0 {
		a.railW = railWidth
	}
	if a.listW <= 0 {
		a.listW = listWidth
	}
	// The workspace fills the window height cleanly so the floating editor card
	// reaches the bottom edge without a full-width footer bar cutting across.
	body := height
	if windowsChrome {
		body -= a.windowsTitleBar(c, t)
	} else if a.narrow {
		body -= topBarHeight
	}
	if body < 0 {
		body = 0
	}

	if a.narrow && !windowsChrome {
		a.topBar(c, t)
	}
	if a.saveErr != "" {
		ui.Text(c, a.statusText()).Height(36).Shrink(0).Padding(8, 14).
			TextColor(signalRed).Background(paperRaised).SingleLine().Tooltip(a.statusText())
		body = max(0, body-36)
	}
	mainRow := ui.Row(c).Height(body).Shrink(0).AlignItems(ui.Stretch).Clip()
	backdrop(c, mainRow)
	mainRow.Children(func() {
		switch {
		case !a.narrow:
			if a.railOpen {
				a.railPane(c, t)
				a.paneSplitter(c, "rail", &a.railW, railWidth, railMinWidth, railMaxWidth, true)
			}
			ui.Column(c).Width(a.listW).Shrink(0).Children(func() {
				if !windowsChrome {
					a.topBar(c, t)
				}
				a.notesPane(c, t)
			})
			a.paneSplitter(c, "list", &a.listW, listWidth, listMinWidth, listMaxWidth, false)
			ui.Column(c).Grow(1).MinWidth(0).Padding(cardGap, cardGap, cardGap, 0).Children(func() {
				a.editorPane(c, t)
			})
		case a.narrowEditor:
			a.editorPane(c, t)
		default:
			a.notesPane(c, t)
		}
	})
	if a.narrow && a.railOpen {
		a.railOverlay(c, t)
	}
}

// paneSplitter draws an interactive vertical resize handle between columns.
// Double-clicking resets the column width to its default.
func (a *nativeApp) paneSplitter(c *ui.Context, key string, w *float32, def, minW, maxW float32, drawLine bool) {
	width := float32(cardGap)
	if drawLine {
		width = 4
	}
	split := ui.Column(c.Key("splitter:" + key)).Width(width).Shrink(0).
		Cursor(ui.CursorResizeColumn)
	if drawLine {
		split.BorderStartWidth(1).BorderColor(hairline)
	}
	if dx, _, ok := split.Dragged(); ok {
		*w = min(max(*w+dx, minW), maxW)
	}
	if split.DoubleClicked() {
		*w = def
	}
	if split.Hovered() || split.Pressed() {
		split.Background(routeBlue.Alpha(0.24))
	}
}

// --- Chrome -----------------------------------------------------------------

// topBar is the upper chrome bar: a Kero-style 44px titlebar row aligned with
// the window traffic lights (sidebar toggle, new note, settings) and a spacious
// search field directly below it.
func (a *nativeApp) topBar(c *ui.Context, t *ui.Theme) {
	bar := c.TitleBar()
	if !a.narrow {
		leftPad := float32(12)
		if !a.railOpen && bar.Left > 0 {
			leftPad = max(12, bar.Left+6)
		}
		headerH := float32(44)
		if bar.Height > headerH {
			headerH = bar.Height
		}
		ui.Column(c).Padding(0, 0, 4, 0).Gap(4).Children(func() {
			ui.Row(c).Height(headerH).Padding(0, 12, 0, leftPad).Gap(4).
				AlignItems(ui.Center).DragWindow().Children(func() {
				if !a.railOpen && bar.Left == 0 {
					mark(c)
					ui.Text(c, "joplin-lite").Font(fontHeading).FontSize(19).TextColor(inkNavy)
				}
				ui.Spacer(c)
				if !a.railOpen {
					if iconButton(c, "top-open-notebooks", iconSidebar, "Open notebooks", "Toggle notebooks sidebar (Cmd/Ctrl+\\)").Clicked() {
						a.railOpen = true
					}
				}
				if iconButton(c, "top-new-note", iconPlus, "New note", "Create a new note (Cmd/Ctrl+N)").Clicked() {
					a.newNote()
				}
				if iconButton(c, "top-settings", iconSettings, "Settings", "Sync target, master password and clipper token (Cmd/Ctrl+,)").Clicked() {
					a.openSettings()
				}
			})
			ui.Row(c).Padding(0, 12).Height(36).Children(func() {
				a.searchField(c, t)
			})
		})
		return
	}
	leftPad := float32(14)
	barH := topBarHeight
	if bar.Left > 0 {
		leftPad = max(14, bar.Left+8)
	}
	if bar.Height > 0 {
		barH = bar.Height
	}
	ui.Row(c).Height(barH).Shrink(0).AlignItems(ui.Center).Gap(10).
		Padding(0, 14, 0, leftPad).DragWindow().
		Background(navyDeep).Children(func() {
		if a.narrow {
			if iconButton(c, "narrow-open-notebooks", iconSidebar, "Open notebooks", "Notebooks and tags").Clicked() {
				a.railOpen = !a.railOpen
			}
		}
		if bar.Left == 0 {
			mark(c)
			ui.Column(c).Gap(0).Shrink(0).Children(func() {
				ui.Text(c, "joplin-lite").Font(fontHeading).FontSize(20).TextColor(inkNavy)
			})
		}
		a.searchField(c, t)
		ui.Spacer(c)
		primary := primaryAction(c, "New note")
		if a.narrow {
			primary.Label("New note")
		}
		if primary.Clicked() {
			a.newNote()
		}
		if iconButton(c, "narrow-settings", iconSettings, "Settings", "Sync target, master password and clipper token").Clicked() {
			a.openSettings()
		}
	})
}

// mark is the folded-ribbon J monogram beside the Humanist wordmark.
func mark(c *ui.Context) ui.Element {
	return ui.Box(c).Size(26, 32).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		x, y := r.X, r.Y
		var cap, stem, base, tailIn, tailOut ui.Path
		cap.MoveTo(x+7, y+3).LineTo(x+24, y+3).LineTo(x+17, y+10).LineTo(x+7, y+10).Close()
		stem.MoveTo(x+17, y+10).LineTo(x+24, y+3).LineTo(x+24, y+23).LineTo(x+17, y+23).Close()
		base.MoveTo(x+2, y+23).LineTo(x+24, y+23).LineTo(x+17, y+30).LineTo(x+9, y+30).Close()
		tailIn.MoveTo(x+9, y+16).LineTo(x+9, y+23).LineTo(x+2, y+23).Close()
		tailOut.MoveTo(x+2, y+16).LineTo(x+9, y+16).LineTo(x+2, y+23).Close()
		p.FillPath(&cap, routeBlue.Alpha(0.36))
		p.FillPath(&stem, routeBlue.Alpha(0.74))
		p.FillPath(&base, routeBlue)
		p.FillPath(&tailIn, routeBlue.Alpha(0.56))
		p.FillPath(&tailOut, routeBlue.Alpha(0.68))
	})
}

// chromeButton is quiet at rest, with a tonal fill on hover.
func chromeButton(c *ui.Context, label string) ui.Element {
	chrome := designTheme()
	chrome.Surface = ui.Transparent
	chrome.SurfaceHover = navyLine
	chrome.SurfacePressed = navyLine
	chrome.Border = ui.Transparent
	chrome.Text = inkNavy
	chrome.TextMuted = navyText
	chrome.InverseText = inkNavy
	return underTheme(c, func() *ui.Theme { return chrome }, func() ui.Element {
		return ui.Button(c, label).Padding(6, 10).Radius(radiusControl)
	})
}

// searchField is the compact Liquid Glass search input, themed so its
// placeholder, caret, selection, and Kero-style focus glow read on the gradient
// backdrop, with the mono kbd hint at its end.
func (a *nativeApp) searchField(c *ui.Context, t *ui.Theme) {
	field := designTheme()
	field.Text = inkNavy
	field.TextMuted = navyText
	field.Surface = ui.Transparent
	field.Accent = ui.Transparent
	field.Focus = ui.Transparent
	field.Selection = routeBlueSoft.Alpha(0.35)
	field.Border = ui.Transparent

	fill := paperRaised.Alpha(0.24)
	stroke := ui.RGBA(15, 29, 20, 0.13)
	if darkPalette {
		fill = ui.RGBA(255, 255, 255, 0.06)
		stroke = ui.RGBA(255, 255, 255, 0.14)
	}
	var focused bool
	box := ui.Box(c).Height(34).Grow(1).MaxWidth(420).Background(fill).
		Material(glass.Glass{Style: glass.Regular, Tint: fill}).Radius(12)
	box.Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Center).Gap(8).Padding(0, 10).Children(func() {
			ui.Icon(c, iconSearch).Size(14, 14).TextColor(navyText).Shrink(0)
			underTheme(c, func() *ui.Theme { return field }, func() ui.Element {
				in := ui.TextInput(c.Key("search"), &a.query).
					Placeholder("Search all notes…").
					Label("Search notes").
					TextColor(inkNavy).
					SelectionColor(routeBlueSoft.Alpha(0.35)).
					Background(ui.Transparent).
					Border(0, ui.Transparent).
					Padding(4, 2).
					Grow(1)
				if a.focusSearch {
					in.Focus()
					a.focusSearch = false
				}
				focused = in.Focused()
				if in.Changed() {
					a.reloadNotes()
					// A narrowed query can drop the open note from the rows,
					// and the reload then opens the first match. That note must
					// not take the keyboard: the user is typing here, and every
					// character after the first would land in its body.
					a.focusBody = false
				}
				if in.Focused() && len(a.notes) > 0 {
					if c.Shortcut(0, ui.KeyDown) {
						next := min(max(a.selectedNote+1, 0), len(a.notes)-1)
						if next != a.selectedNote {
							a.selectedNote = next
							a.loadNote()
							a.list.ScrollIntoView(next)
							a.focusBody = false
						}
					} else if c.Shortcut(0, ui.KeyUp) {
						prev := min(max(a.selectedNote-1, 0), len(a.notes)-1)
						if prev != a.selectedNote {
							a.selectedNote = prev
							a.loadNote()
							a.list.ScrollIntoView(prev)
							a.focusBody = false
						}
					}
				}
				return in
			})
			if a.query != "" {
				if iconButton(c, "search-clear", iconClose, "Clear search", "Clear search").Clicked() {
					a.query = ""
					a.reloadNotes()
				}
			}
			saved := iconButton(c, "saved-searches", iconBookmark, "Saved searches", "Saved searches")
			if saved.Clicked() {
				a.savedMenuOpen = !a.savedMenuOpen
			}
			a.menu(c, saved, &a.savedMenuOpen, "saved", 250, func() { a.savedSearchMenu(c, t) })
			kbd := ui.Row(c.Key("search-kbd")).Shrink(0).Height(20).Radius(radiusSmall).Border(1, stroke).
				Padding(0, 6).AlignItems(ui.Center).
				Label("Command palette").
				Tooltip("Command palette & quick switcher (Cmd/Ctrl+P)")
			if kbd.Hovered() {
				kbd.Background(paperSunken.Alpha(0.75))
			}
			if kbd.Clicked() {
				a.openPalette()
			}
			kbd.Children(func() {
				label := "⌘K"
				if runtime.GOOS != "darwin" {
					label = "Ctrl+K"
				}
				ui.Text(c, label).Font(fontMono).FontFeatures("tnum").FontSize(sizeMonoSm).TextColor(navyText)
			})
		})
	})
	glow := box.Animate("glow", boolF(focused), 120*time.Millisecond)
	if glow > 0 {
		box.Shadow(0, 0, 0, 3.5*glow, routeBlue.Alpha(0.28*glow)).
			Border(1, routeBlue.Alpha(0.70*glow))
	}
}

// sidebarFooter is the quiet status line tucked at the bottom of the left note
// column: sync state on the left and a compact sync trigger on the right.
func (a *nativeApp) sidebarFooter(c *ui.Context, t *ui.Theme) {
	st := a.syncState()
	notice := a.syncNotice()

	ui.Row(c).Height(32).Shrink(0).AlignItems(ui.Center).Gap(8).Padding(0, 12).
		BorderWidth(1, 0, 0, 0).BorderColor(hairline.Alpha(0.65)).Children(func() {
		dot := ui.Box(c).Size(7, 7).Radius(3.5).Shrink(0)
		dot.Background(statusDotColor(st, notice))
		line := ui.Text(c, notice.Line).FontSize(sizeSmall).TextColor(inkSoft).SingleLine().MinWidth(0).Grow(1)
		if notice.Hint != "" {
			dot.Label(notice.Hint).Tooltip(notice.Hint)
			line.Tooltip(notice.Hint)
		}
		if !st.Enabled {
			if chromeButton(c, "Set up sync").Tooltip("Choose a sync target").Clicked() {
				a.openSettings()
			}
		} else if iconButton(c, "footer-sync-now", iconSync, "Sync now", "Synchronize now (Cmd/Ctrl+R)").Disabled(st.Running || st.Pending).Clicked() {
			a.syncNow()
		}
	})
}

// statusDotColor is the one-word state of the strip, in the service
// vocabulary: moss synced, route blue moving, amber waiting, red disrupted.
func statusDotColor(st syncSnapshot, notice syncNotice) ui.Color {
	switch {
	case !st.Enabled:
		return navyLine
	case st.Running || st.Pending:
		return routeBlueSoft
	case notice.Err:
		return signalRedSoft
	case strings.HasPrefix(notice.Line, "Sync paused"), strings.HasPrefix(notice.Line, "Offline"):
		return amberBright
	case st.Last.IsZero():
		return navyText
	default:
		return mossBright
	}
}

// --- Rail -------------------------------------------------------------------

// chosenRail is the rail's current choice: the tag filter when one is set,
// else the notebook (or the virtual all/conflicts entries).
func (a *nativeApp) chosenRail() string {
	if a.selectedTag != "" {
		return "tag:" + a.selectedTag
	}
	return a.selectedNotebook
}

// railPane is the notebook rail: every notebook as a tree, its tags below, and
// the counts in mono at the end of each row.
func (a *nativeApp) railPane(c *ui.Context, t *ui.Theme) {
	w := a.railW
	if w <= 0 || a.narrow {
		w = railWidth
	}
	pane := ui.Column(c).Width(w)
	if a.narrow {
		pane.Grow(1)
	} else {
		tint := paperRaised.Alpha(0.24)
		if darkPalette {
			tint = ui.RGBA(0, 0, 0, 0.16)
		}
		pane.FillHeight().Shrink(0).Background(tint).
			Material(glass.Glass{Style: glass.Clear, Tint: tint})
	}
	bar := c.TitleBar()
	headerH := float32(44)
	if bar.Height > headerH {
		headerH = bar.Height
	}
	if windowsChrome {
		headerH = 36
	}
	leftPad := float32(14)
	if !a.narrow && bar.Left > 0 {
		leftPad = max(14, bar.Left+6)
	}
	pane.Children(func() {
		if !a.narrow {
			ui.Row(c).Height(headerH).Padding(0, 10, 0, leftPad).AlignItems(ui.Center).DragWindow().Children(func() {
				if windowsChrome {
					ui.Text(c, "Notebooks").FontSize(sizeUI).Bold().TextColor(inkNavy)
				} else if bar.Left == 0 {
					mark(c)
					ui.Spacer(c).Width(6)
					ui.Text(c, "joplin-lite").Font(fontHeading).FontSize(18).TextColor(inkNavy)
				}
				ui.Spacer(c)
				if iconButton(c, "close-notebooks", iconSidebar, "Close notebooks", "Hide notebooks sidebar (Cmd/Ctrl+\\)").Clicked() {
					a.railOpen = false
				}
			})
		}
		hdr := ui.Row(c).Padding(6, 10, 6, 14).Gap(4).AlignItems(ui.Center)
		hdr.Children(func() {
			label(c, "Notebooks")
			ui.Text(c, fmt.Sprintf("%d", len(a.notebooks))).Font(fontMono).FontFeatures("tnum").
				FontSize(sizeMonoSm).TextColor(inkSoft)
			ui.Spacer(c)
			if iconButton(c, "new-notebook", iconPlus, "New notebook", "New notebook").Clicked() {
				a.openNewNotebook()
			}
			if a.notebookByID(a.selectedNotebook) != nil {
				actions := iconButton(c, "notebook-actions", iconMore, "Notebook actions", "Rename or delete this notebook")
				if actions.Clicked() {
					a.notebookActions = !a.notebookActions
				}
				a.menu(c, actions, &a.notebookActions, "notebook", 190, func() {
					a.notebookMenu(c, t)
				})
			}
			if a.narrow {
				if iconButton(c, "close-notebooks", iconClose, "Close notebooks", "Close").Clicked() {
					a.railOpen = false
				}
			}
		})
		a.notebookRows(c, t)
		hairlineRule(c)
		ui.Row(c).Padding(8, 12, 8, 14).Gap(4).AlignItems(ui.Center).Children(func() {
			label(c, "Tags")
			ui.Spacer(c)
			if iconButton(c, "new-tag", iconPlus, "New tag", "New tag").Clicked() {
				a.openNewTag()
			}
			if a.selectedTag != "" {
				actions := iconButton(c, "tag-actions", iconMore, "Tag actions", "Rename or delete this tag")
				if actions.Clicked() {
					a.tagActions = !a.tagActions
				}
				a.menu(c, actions, &a.tagActions, "tag-actions", 170, func() {
					a.tagActionsMenu(c, t)
				})
				if iconButton(c, "clear-tag", iconClose, "Clear tag", "Clear the tag filter").Clicked() {
					a.selectedTag, a.selectedID = "", ""
					a.reloadNotes()
				}
			}
		})
		a.tagRows(c, t)
	})
}

// notebookRows is the notebook tree: one 32px row per notebook, indented by
// depth with a hairline spine, chosen in accent, dragging a note onto a row
// moving it there.
func (a *nativeApp) notebookRows(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c).Grow(1).Padding(4, 8).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			a.railRow(c, t, "all", "", "All notes", "", -1, 0)
			a.railRow(c, t, "conflicts", conflictsRail, "Conflicts", "", a.conflictCount, 0)
			var walk func(nodes []*models.Folder)
			walk = func(nodes []*models.Folder) {
				for _, nb := range nodes {
					a.railRow(c, t, nb.ID, nb.ID, nb.Title, "", nb.NoteCount, a.notebookDepth[nb.ID])
					walk(nb.Children)
				}
			}
			walk(a.notebookTree)
		})
	})
}

// tagRows lists the tags, chosen the same way; the list is capped so many tags
// cannot take the rail over.
func (a *nativeApp) tagRows(c *ui.Context, t *ui.Theme) {
	// One 32px row and its hairline each, plus the scroll's own padding: a
	// short section that still fits every tag it shows.
	height := float32(navRowHeight+1) * float32(len(a.tags))
	height += 10
	if height < 36 {
		height = 36
	}
	if height > 176 {
		height = 176
	}
	ui.Scroll(c).Height(height).Shrink(0).Padding(4, 8, 6, 8).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			if len(a.tags) == 0 {
				ui.Text(c, "No tags yet").FontSize(sizeLabel).TextColor(inkSoft).Padding(4, 6)
				return
			}
			for _, tag := range a.tags {
				a.railRow(c, t, "tag:"+tag.ID, "tag:"+tag.ID, tag.Title, "#", -1, 0)
			}
		})
	})
}

// railRow is one row of the rail. id is what choosing it selects ("" for All
// notes); count is drawn in mono at its end.
func (a *nativeApp) railRow(c *ui.Context, t *ui.Theme, key, id, text, flag string, count, depth int) {
	chosen := a.chosenRail() == id || (id == "" && key == "all" && a.chosenRail() == "")
	isTag := strings.HasPrefix(key, "tag:")
	tagID := strings.TrimPrefix(key, "tag:")
	isNotebook := id != "" && id != conflictsRail && !isTag
	renaming := (isNotebook && a.inlineRenameKind == "notebook" && a.inlineRenameID == id) ||
		(isTag && a.inlineRenameKind == "tag" && a.inlineRenameID == tagID)

	row := ui.Row(c.Key("rail:" + key)).Height(navRowHeight).MinHeight(navRowHeight).
		Radius(radiusControl).AlignItems(ui.Center).Gap(6).Shrink(0).
		PaddingStart(8 + float32(depth)*14).PaddingEnd(8)
	hover := row.Animate("hover", boolF(row.Hovered()), motionHover.Duration)
	chosenPill(row, chosen, hover > 0.002)

	if depth > 0 {
		// The tree's hairline spine.
		row.Draw(func(p *ui.Painter, r ui.Rect) {
			x := r.X + float32(depth)*14 - 4
			p.Line(x, r.Y, x, r.Y+r.H, 1, hairline)
		})
	}
	if _, over := ui.DragOver[[]string](row); over {
		row.Background(routeBlue.Alpha(0.18))
	}
	if ids, ok := ui.Drop[[]string](row); ok {
		a.moveNotes(ids, id)
	}
	if isNotebook {
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("New note here").Chosen() {
				a.selectedTag, a.selectedNotebook, a.selectedID = "", id, ""
				a.reloadNotes()
				a.newNote()
			}
			if m.Item("New sub-notebook…").Chosen() {
				a.selectedTag, a.selectedNotebook, a.selectedID = "", id, ""
				a.reloadNotes()
				a.openNewNotebook()
			}
			if m.Item("Rename notebook…").Chosen() {
				a.selectedTag, a.selectedNotebook, a.selectedID = "", id, ""
				a.reloadNotes()
				a.openRenameNotebook()
			}
			m.Separator()
			if m.Item("Delete notebook…").Chosen() {
				a.selectedTag, a.selectedNotebook, a.selectedID = "", id, ""
				a.reloadNotes()
				a.confirmDeleteNotebook = true
			}
		})
	} else if isTag && tagID != "" {
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("Rename tag…").Chosen() {
				a.selectedNotebook, a.selectedTag, a.selectedID = "", tagID, ""
				a.reloadNotes()
				a.openRenameTag()
			}
			if a.selectedTag == tagID {
				if m.Item("Clear tag filter").Chosen() {
					a.selectedTag, a.selectedID = "", ""
					a.reloadNotes()
				}
			}
			m.Separator()
			if m.Item("Delete tag…").Chosen() {
				a.selectedNotebook, a.selectedTag, a.selectedID = "", tagID, ""
				a.reloadNotes()
				a.confirmDeleteTag = true
			}
		})
	}
	if !renaming {
		row.Label(text)
	}
	doubleClicked := !renaming && row.DoubleClicked()
	clicked := !renaming && row.Clicked()
	row.Children(func() {
		var rowIcon *ui.SVG
		switch {
		case key == "all":
			rowIcon = iconNotes
		case key == "conflicts":
			rowIcon = iconAlert
		case flag == "#":
			rowIcon = iconTag
		default:
			rowIcon = iconFolder
		}
		iconColor := inkSoft
		if key == "conflicts" && count > 0 {
			iconColor = amberInk
		} else if chosen {
			iconColor = inkNavy
		}
		ui.Icon(c, rowIcon).Size(13, 13).TextColor(iconColor).Shrink(0)
		if renaming {
			in := ui.TextInput(c.Key("inline-rename:"+key), &a.inlineRenameTitle).
				Label("Inline rename").
				FontSize(sizeUI).
				Padding(2, 6).
				Grow(1).
				AutoFocus()
			if a.inlineRenameFocus {
				in.Focus()
				a.inlineRenameFocus = false
			}
			if in.Submitted() {
				a.commitInlineRename()
			} else if c.Shortcut(0, ui.KeyEscape) {
				a.cancelInlineRename()
			}
			return
		}
		if flag == "#" {
			ui.Text(c, "#").FontSize(sizeSmall).TextColor(inkSoft)
		}
		name := ui.Text(c, text).FontSize(sizeUI).SingleLine().Grow(1)
		if chosen {
			name.Bold()
		}
		if count > 0 {
			countColor := inkSoft
			if key == "conflicts" {
				countColor = amberInk
			}
			ui.Text(c, fmt.Sprintf("%d", count)).Font(fontMono).FontFeatures("tnum").FontSize(sizeMonoSm).TextColor(countColor)
		}
	})
	if doubleClicked {
		if isNotebook {
			a.selectedTag, a.selectedNotebook, a.selectedID = "", id, ""
			a.reloadNotes()
			a.startInlineRename("notebook", id, text)
			return
		} else if isTag && tagID != "" {
			a.selectedNotebook, a.selectedTag, a.selectedID = "", tagID, ""
			a.reloadNotes()
			a.startInlineRename("tag", tagID, text)
			return
		}
	}
	if clicked {
		switch {
		case key == "conflicts":
			a.selectedTag, a.selectedNotebook, a.selectedID = "", conflictsRail, ""
		case key == "all":
			a.selectedTag, a.selectedNotebook, a.selectedID = "", "", ""
		case strings.HasPrefix(key, "tag:"):
			a.selectedNotebook, a.selectedTag, a.selectedID = "", id[len("tag:"):], ""
		default:
			a.selectedTag, a.selectedNotebook, a.selectedID = "", id, ""
		}
		a.reloadNotes()
		if a.narrow {
			a.railOpen = false
		}
	}
}

// railOverlay is the slide-over the rail becomes below the breakpoint.
func (a *nativeApp) railOverlay(c *ui.Context, t *ui.Theme) {
	run := a.surfaces.opening("rail", a.railOpen)
	ui.DialogBase(c, &a.railOpen, func(back, panel ui.Element) {
		back.Background(inkNavy.Alpha(0.24))
		v := panel.Keyframes("rail-slide", []ui.Keyframe{
			{At: 0, Value: 0},
			{At: 220 * time.Millisecond, Value: 1, Ease: ui.EaseOut},
		}, ui.KeyframeOptions{Run: run})
		// The panel is the whole window, laid out as a row; only the rail
		// inside it paints, and the rest of the row is the way out, as a
		// backdrop is for a dialog. Sliding the rail itself, rather than an
		// absolutely placed panel, keeps the list behind it laid out as it is.
		panel.Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(ui.Transparent).Radius(0)
		panel.Children(func() {
			ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
				tint := paperSunken.Alpha(0.84)
				rail := ui.Column(c).Width(railWidth).Shrink(0).Background(tint).
					Material(glass.Glass{Style: glass.Regular, Tint: tint}).
					Translate((v-1)*railWidth, 0)
				rail.Children(func() {
					a.railPane(c, t)
				})
				// The rest of the window is the way out, as a dialog's backdrop.
				outside := ui.Box(c).Grow(1).Label("Close the notebook drawer")
				if outside.Clicked() {
					a.railOpen = false
				}
			})
		})
	})
}

// --- Note list --------------------------------------------------------------

// notesPane is the middle pane: the list's toolbar, the bulk-action bar while
// rows are chosen, and the rows themselves.
func (a *nativeApp) notesPane(c *ui.Context, t *ui.Theme) {
	width := a.listW
	if width <= 0 {
		width = listWidth
	}
	if a.narrow {
		width = 0
	}
	pane := ui.Column(c).Grow(1).MinHeight(0)
	if width > 0 {
		pane.Width(width)
	} else {
		pane.Grow(1)
	}
	pane.Children(func() {
		ui.Row(c).Padding(4, 10, 4, 14).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.listTitle()).FontSize(sizeSmall).Bold().TextColor(inkSoft).SingleLine()
			ui.Text(c, fmtCount(len(a.notes), a.hasMore)).Font(fontMono).FontFeatures("tnum").
				FontSize(sizeMonoSm).TextColor(inkSoft)
			ui.Spacer(c)
			sortButton := iconButton(c, "sort-notes", iconSort, "Sort", "Sort the note list")
			if sortButton.Clicked() {
				a.sortOpen = !a.sortOpen
			}
			a.menu(c, sortButton, &a.sortOpen, "sort", 190, func() { a.sortMenu(c, t) })
		})
		if n := a.selection.Len(); n > 1 {
			ui.Row(c).Key("bulk-bar").Padding(2, 12, 6, 12).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("%d selected", n)).Font(fontMono).FontFeatures("tnum").FontSize(sizeMonoSm).
					TextColor(violet).Grow(1)
				move := ui.Button(c, "Move…").Label("Move selected notes").
					Tooltip("Move the selected notes")
				move.Padding(4, 8)
				if move.Clicked() {
					a.moveFilter = ""
					a.bulkMoveOpen = !a.bulkMoveOpen
				}
				a.menu(c, move, &a.bulkMoveOpen, "bulk-move", 240, func() { a.bulkMoveMenu(c, t) })
				deleteBtn := ui.Button(c, "Delete…").Label("Delete selected notes").
					Tooltip("Delete the selected notes")
				deleteBtn.Padding(4, 8)
				if deleteBtn.Clicked() {
					a.confirmBulkDelete = true
				}
				clear := ui.Button(c, "Clear").Label("Clear selection")
				clear.Padding(4, 8)
				if clear.Clicked() {
					a.selection.Clear()
				}
			})
		}
		a.noteRows(c, t)
		a.sidebarFooter(c, t)
	})
}

// listTitle is the heading of the note list: the notebook, tag or virtual
// folder the rows come from.
func (a *nativeApp) listTitle() string {
	switch {
	case a.selectedNotebook == conflictsRail:
		return "Conflicts"
	case a.selectedTag != "":
		if tag := a.tagByID(a.selectedTag); tag != nil {
			return "# " + tag.Title
		}
	case a.selectedNotebook != "":
		if nb := a.notebookByID(a.selectedNotebook); nb != nil {
			return nb.Title
		}
	}
	if a.query != "" {
		return "Search results"
	}
	return "All notes"
}

// noteRows is the list itself: a virtualized ListState whose row is the
// timetable row of DESIGN.md — hairline ruled, a flag column, a title and meta
// stack, and the updated time in mono at the end.
func (a *nativeApp) noteRows(c *ui.Context, t *ui.Theme) {
	if len(a.notes) == 0 {
		a.emptyList(c, t)
		return
	}
	// The toolkit's list keeps a choice by key when rows move under it, so a
	// note that lands at the index the open note just left would keep the
	// pill on a note that is not in the editor. Correct a wrong row; a list
	// with no choice is left to make one, as clicking a row to open it does.
	if a.selectedNote >= 0 && a.selectedNote < len(a.notes) && a.notes[a.selectedNote].ID != a.selectedID {
		a.selectedNote = a.rowOf(a.selectedID)
	}
	a.list.Selected = &a.selectedNote
	a.list.Selection = &a.selection
	a.list.Key = func(i int) any {
		if i < 0 || i >= len(a.notes) {
			return i
		}
		return a.notes[i].ID
	}
	a.list.Label = func(i int) string {
		if i < 0 || i >= len(a.notes) {
			return ""
		}
		return noteRowTitle(a.notes[i])
	}
	// Dragging a chosen row drags the whole choice; an unchosen row drags
	// only itself. The drag starts on the row's grip: making the whole row
	// a drag source would take the press from the list, and a click would
	// stop choosing a row.
	dragIDs := a.selectedIDs()
	var postListAction func()
	if a.hasPendingScroll {
		a.list.ScrollTo(a.pendingScrollRow, ui.Start)
		a.hasPendingScroll = false
	}
	// The list chooses rows with the theme's accent, so it is built under the
	// row theme: a violet tint, not a solid fill (DESIGN.md's chosen row).
	list := underTheme(c, listTheme, func() ui.Element {
		return ui.List(c.Key("note-list"), &a.list, len(a.notes), func(i int) {
			if i >= 0 && i < len(a.notes) {
				a.noteRow(c, t, i, dragIDs, &postListAction)
			}
		}).Padding(2, 8)
	}).Grow(1)
	if postListAction != nil {
		postListAction()
	}
	if list.Changed() {
		if a.narrow && c.Modifiers()&(ui.Cmd|ui.Shift) == 0 {
			a.narrowEditor = true
		}
		a.loadNote()
		if a.narrow && !a.narrowEditor {
			a.focusBody = false
		}
	}
	if a.hasMore || a.lastLoadedCount > 0 {
		bar := ui.Row(c).Padding(6, 12).Gap(8).AlignItems(ui.Center).Shrink(0)
		if a.loadMoreRun > 0 {
			pulse := bar.Keyframes("load-more-bar", []ui.Keyframe{
				{At: 0, Value: 1},
				{At: 550 * time.Millisecond, Value: 0, Ease: ui.EaseOut},
			}, ui.KeyframeOptions{Run: a.loadMoreRun})
			if pulse > 0.01 {
				bar.Background(violet.Alpha(0.10 * pulse)).Radius(radiusControl)
			}
		}
		bar.Children(func() {
			if a.hasMore {
				moreTint := routeBlueSoft.Alpha(0.18)
				more := ui.Button(c, "Load more").Label("Load more notes").Radius(13).Padding(5, 11).
					Material(glass.Glass{Style: glass.Regular, Tint: moreTint, Interactive: true})
				if more.Clicked() {
					a.loadMore()
				}
			}
			if a.lastLoadedCount > 0 {
				msg := fmt.Sprintf("+%d loaded", a.lastLoadedCount)
				if !a.hasMore {
					msg = fmt.Sprintf("+%d loaded · all %d shown", a.lastLoadedCount, len(a.notes))
				}
				badge := ui.Text(c, msg).Font(fontMono).FontFeatures("tnum").FontSize(sizeMonoSm).TextColor(inkSoft)
				if a.loadMoreRun > 0 {
					appear := badge.Keyframes("load-more-badge", []ui.Keyframe{
						{At: 0, Value: 0},
						{At: 220 * time.Millisecond, Value: 1, Ease: ui.EaseOut},
					}, ui.KeyframeOptions{Run: a.loadMoreRun})
					badge.Opacity(0.2+0.8*appear).Translate(0, (1-appear)*4)
				}
			}
		})
	}
}

// noteRow draws one row of the list: hairline ruled, a flag column, a title
// and date header with the excerpt below, and, for the note the editor
// follows, the sage tint and left accent bar that mark a choice (MyGO's own
// List paints only its multi-selection, so the current row is drawn here).
func (a *nativeApp) noteRow(c *ui.Context, t *ui.Theme, i int, dragIDs []string, postListAction *func()) {
	n := a.notes[i]
	title := noteRowTitle(n)
	current := a.note != nil && i == a.selectedNote
	bulk := a.selection.Has(n.ID)
	row := ui.Row(c).Padding(6, 14).Gap(8).AlignItems(ui.Center).MinHeight(rowHeight).Key(n.ID)
	row.ContextMenu(func(m *ui.Menu) {
		selectNote := func() {
			if a.selectedNote != i || a.note == nil || a.note.ID != n.ID {
				a.selectedNote = i
				a.selection.Clear()
				a.loadNote()
			}
		}
		editLabel := "Edit note"
		if current && a.editing {
			editLabel = "Preview note"
		}
		if m.Item(editLabel).Chosen() {
			*postListAction = func() {
				selectNote()
				a.toggleEdit()
			}
		}
		todoLabel := "Mark as Todo"
		if n.IsTodo != 0 {
			todoLabel = "Convert to regular note"
		}
		if m.Item(todoLabel).Chosen() {
			*postListAction = func() {
				selectNote()
				a.setTodo(n.IsTodo == 0)
			}
		}
		if len(a.notebooks) > 0 {
			m.Submenu("Move to notebook", func(sub *ui.Menu) {
				for _, nb := range a.notebooks {
					nb := nb
					if sub.Item(nb.Title).Checked(nb.ID == n.ParentID).Chosen() {
						*postListAction = func() {
							a.moveNotes([]string{n.ID}, nb.ID)
						}
					}
				}
			})
		}
		if len(a.tags) > 0 {
			m.Submenu("Tags", func(sub *ui.Menu) {
				for _, tag := range a.tags {
					tag := tag
					checked := current && a.hasNoteTag(tag.ID)
					if sub.Item("#" + tag.Title).Checked(checked).Chosen() {
						*postListAction = func() {
							selectNote()
							a.toggleTag(tag.ID)
						}
					}
				}
			})
		}
		m.Separator()
		if m.Item("Delete note…").Chosen() {
			*postListAction = func() {
				selectNote()
				a.confirmDelete = true
			}
		}
	})
	hover := row.Animate("hover", boolF(row.Hovered()), motionHover.Duration)
	row.Radius(radiusControl).Padding(0, 10, 0, 8)
	var loadFlash float32
	if a.loadedFrom > 0 && i >= a.loadedFrom && a.loadMoreRun > 0 {
		stagger := time.Duration(min(i-a.loadedFrom, 10)) * 20 * time.Millisecond
		enter := row.Keyframes("load-enter", []ui.Keyframe{
			{At: 0, Value: 0},
			{At: 260 * time.Millisecond, Value: 1, Ease: ui.EaseOut},
		}, ui.KeyframeOptions{Run: a.loadMoreRun, Delay: stagger})
		loadFlash = row.Keyframes("load-flash", []ui.Keyframe{
			{At: 0, Value: 1},
			{At: 650 * time.Millisecond, Value: 0, Ease: ui.EaseOut},
		}, ui.KeyframeOptions{Run: a.loadMoreRun, Delay: stagger})
		row.Translate(0, (1-enter)*8).Opacity(0.25 + 0.75*enter)
	}
	if bulk && !current {
		row.Background(violet.Alpha(0.11 + 0.04*hover))
	} else {
		chosenPill(row, current, hover > 0.002)
		if !current && loadFlash > 0.01 {
			row.Background(violet.Alpha(0.10 * loadFlash))
		}
	}

	row.Children(func() {
		if a.railOpen {
			grip := ui.Text(c, "⣿").FontSize(sizeMonoSm).TextColor(hairlineStrong).Shrink(0)
			grip.Label("Drag note " + title).Tooltip("Drag to a notebook")
			if a.selection.Has(n.ID) && len(dragIDs) > 0 {
				grip.Drag(dragIDs)
			} else {
				grip.Drag([]string{n.ID})
			}
			opacity := float32(0.25)
			if hover > 0.2 {
				opacity = 0.35 + 0.65*hover
			}
			grip.Opacity(opacity)
		}

		if n.IsConflict != 0 {
			ui.Text(c, "!").FontSize(sizeSmall).Bold().TextColor(signalRed).Label("Conflict").Shrink(0)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			name := ui.Text(c, title).FontSize(sizeUI).LineHeight(1.25).SingleLine()
			if current || bulk {
				name.FontWeight(600).TextColor(inkNavy)
			} else {
				name.TextColor(inkNavy.Alpha(0.86))
			}
			preview := excerpt(n.Excerpt)
			if preview == "" {
				preview = "Empty note"
			}
			ui.Text(c, preview).FontSize(sizeLabel).LineHeight(1.25).TextColor(inkSoft).SingleLine()
		})
	})
}

// emptyList is the list with nothing in it: the filter is named, and the way
// out of it is one action away.
func (a *nativeApp) emptyList(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Padding(24).Children(func() {
		switch {
		case a.query != "":
			ui.Text(c, "No notes match “"+clip(a.query, 40)+"”").FontSize(sizeUI).TextColor(inkSoft)
			if ui.Button(c, "Clear search").Clicked() {
				a.query = ""
				a.reloadNotes()
			}
		case a.selectedNotebook == conflictsRail:
			ui.Text(c, "No conflicts").FontSize(sizeUI).TextColor(inkSoft)
			ui.Text(c, "No conflicts found on this device.").
				FontSize(sizeLabel).TextColor(inkSoft)
		default:
			ui.Text(c, "No notes here yet").FontSize(sizeUI).TextColor(inkSoft)
			ui.Text(c, "Use New note above to start writing.").FontSize(sizeLabel).TextColor(inkSoft)
			if a.narrow && a.selectedNotebook == "" && a.selectedTag == "" {
				ui.Text(c, "Notes save automatically on this device. Set up sync to keep another copy.").
					MaxWidth(320).FontSize(sizeUI).TextColor(inkSoft)
			}
		}
	})
}

// --- Editor -----------------------------------------------------------------

// editorPane is the work surface: the note's title, its state, its tags, and
// the Markdown editor or the rendered preview of its body.
func (a *nativeApp) editorPane(c *ui.Context, t *ui.Theme) {
	pane := ui.Column(c).Grow(1).MinWidth(0).Background(paperRaised)
	if !a.narrow {
		elevateCard(pane)
	}
	if a.note != nil {
		if files := pane.DroppedFiles(); len(files) > 0 {
			a.attachFiles(files)
		}
		if pane.FileDragOver() {
			pane.Border(2, routeBlue)
		}
	}
	pane.Children(func() {
		a.editorHeader(c, t)
		if a.note == nil {
			a.emptyEditor(c, t)
			return
		}
		if a.editing {
			// Keep the writing surface quiet; code remains monospace in preview.
			svc := c.Services()
			editor := ui.TextArea(c.Key("body:"+a.note.ID), &a.body).
				Placeholder("Write in Markdown…").
				Label("Note body").
				Font(fontSans).
				FontSize(sizeBody).
				LineHeight(1.9).
				Padding(18, 48, 32, 48).
				Background(paperRaised).
				Border(0, ui.Transparent).
				FillWidth().
				Grow(1)
			editor.OnPaste(func(text string) bool {
				if text != "" {
					return false
				}
				data := svc.ReadClipboardData(transfer.FileList, transfer.PNG)
				if files, err := data.Files(); err == nil && len(files) > 0 {
					return a.queuePasteFiles(files)
				}
				if pngBytes, err := data.Read(transfer.PNG); err == nil && len(pngBytes) > 0 {
					return a.queuePasteBytes("pasted-image.png", "image/png", pngBytes)
				}
				return false
			})
			if a.pendingCaret != nil {
				editor.SetTextSelection(*a.pendingCaret, *a.pendingCaret)
				a.pendingCaret = nil
			}
			if a.pendingInsert != "" {
				start, end := editor.TextSelection()
				body := []rune(a.body)
				a.body = string(body[:start]) + a.pendingInsert + string(body[end:])
				caret := start + len([]rune(a.pendingInsert))
				a.pendingCaret = &caret
				a.pendingInsert = ""
				a.markDirty()
				a.flush()
				// The next build loads the replacement into the native editor,
				// then places its caret after the inserted reference.
				svc.Invalidate()
			}
			if a.focusBody {
				editor.SetTextSelection(0, 0).AutoFocus()
				a.focusBody = false
			}
			if editor.Changed() {
				a.markDirty()
			}
			return
		}
		ui.Box(c.Key("preview-box")).Grow(1).MinHeight(0).Children(func() {
			ui.Scroll(c).Fill().Padding(20, 48, 48, 48).Children(func() {
				ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
					ui.Column(c).Grow(1).MaxWidth(700).Gap(20).Children(func() {
						if a.body == "" {
							ui.Text(c, "This note is empty.").FontSize(sizeBody).TextColor(inkSoft)
							return
						}
						a.md.Theme = &markdown.Theme{Font: fontSans, FontSize: sizeBody, LineHeight: 1.9, CodeFont: fontMono}
						a.md.OnLink = nil
						if strings.Contains(a.body, "](:/") {
							a.md.OnLink = a.openLink
						}
						markdown.View(c, a.body, a.md)
					})
				})
			})
			ui.Box(c.Key("preview-edge")).Absolute().Top(0).Left(0).Right(0).Height(18).PassThrough().
				Material(glass.ScrollEdge{Background: paperRaised})
		})
	})
}

// editorHeader separates context labels from actions so long notebook and tag
// names cannot hide controls, with the title directly above the note body.
func (a *nativeApp) editorHeader(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Shrink(0).Children(func() {
		if a.note == nil {
			ui.Row(c).Padding(16, 48, 8, 48).Gap(10).AlignItems(ui.Center).Children(func() {
				if a.narrow {
					if iconButton(c, "narrow-back-notes", iconChevronLeft, "Back to notes", "Back to the note list").Clicked() {
						a.flush()
						a.narrowEditor = false
						a.selectedNote = -1
					}
				}
				ui.Text(c, "No note selected").FontSize(sizeTitle).Bold().Grow(1)
			})
			return
		}

		// Context controls shrink independently of the separate action row.
		var move, tags ui.Element
		ui.Row(c).Padding(12, 24, 4, 24).Gap(6).AlignItems(ui.Center).Children(func() {
			if a.narrow {
				if iconButton(c, "narrow-back-notes", iconChevronLeft, "Back to notes", "Back to the note list").Clicked() {
					a.flush()
					a.narrowEditor = false
					a.selectedNote = -1
				}
			}
			nbName := "Unfiled"
			if nb := a.notebookByID(a.note.ParentID); nb != nil {
				nbName = clip(nb.Title, 22)
			}
			move = toolbarIconButton(c, "hdr-move", iconFolder, nbName, "Move note", "Move to another notebook").Grow(1).Shrink(1).MinWidth(0)
			if move.Clicked() {
				a.moveFilter = ""
				a.moveOpen = !a.moveOpen
			}

			tagLabel := "Tags"
			if len(a.noteTags) == 1 {
				tagLabel = clip(a.noteTags[0].Title, 18)
			} else if len(a.noteTags) == 2 {
				tagLabel = clip(a.noteTags[0].Title, 12) + " · " + clip(a.noteTags[1].Title, 12)
			} else if len(a.noteTags) > 2 {
				tagLabel = fmt.Sprintf("%s +%d", clip(a.noteTags[0].Title, 14), len(a.noteTags)-1)
			}
			tags = toolbarIconButton(c, "hdr-tags", iconTag, tagLabel, "Note tags", "Tags on this note").Grow(1).Shrink(1).MinWidth(0)
			if tags.Clicked() {
				a.tagFilter = ""
				a.tagMenuOpen = !a.tagMenuOpen
			}
		})
		ui.Row(c).Padding(0, 24, 4, 24).Gap(6).AlignItems(ui.Center).Children(func() {
			words := len(strings.Fields(a.body))
			metaText := fmt.Sprintf("%s  ·  %d words", time.UnixMilli(a.note.UpdatedTime).Format("02 Jan 2006 15:04"), words)
			ui.Text(c, metaText).Font(fontMono).FontFeatures("tnum").FontSize(sizeMonoSm).
				TextColor(inkSoft).SingleLine().MinWidth(0).Grow(1)

			todoBtn := iconButton(c, "hdr-todo", iconCheck, "Todo", "Mark as Todo")
			if a.todo {
				todoBtn.Background(violet.Alpha(0.20)).Border(1, violet.Alpha(0.38))
			}
			if todoBtn.Clicked() {
				a.setTodo(!a.todo)
			}

			if iconButton(c, "hdr-attach", iconPaperclip, "Attach file", "Attach an image or file to this note").Clicked() {
				a.promptAttachFiles()
			}

			modeIcon := iconEdit
			if a.editing {
				modeIcon = iconEye
			}
			if iconButton(c, "hdr-mode", modeIcon, a.modeLabel(), "Edit/Preview (Cmd/Ctrl+E)").Clicked() {
				a.toggleEdit()
			}

			del := iconButton(c, "hdr-delete", iconTrash, "Delete note", "Delete note")
			if del.Hovered() {
				del.Background(signalRedSoft)
			}
			if del.Clicked() {
				a.confirmDelete = true
			}
		})

		// Row 2: Borderless Optima title sitting directly above the note body.
		ui.Row(c).Padding(12, 48, 4, 48).AlignItems(ui.Center).Children(func() {
			title := ui.TextInput(c.Key("title:"+a.note.ID), &a.title).
				Placeholder("Untitled").
				Label("Note title").
				FontSize(sizeTitle).
				Font(fontHeading).
				Padding(0).
				Background(ui.Transparent).
				Border(0, ui.Transparent).
				Grow(1)
			if title.Changed() {
				a.markDirty()
			}
		})

		if a.note.IsConflict != 0 {
			ui.Row(c).Padding(4, 48, 4, 48).Gap(6).AlignItems(ui.Center).Children(func() {
				chip := ui.Row(c).Gap(4).AlignItems(ui.Center).Radius(radiusControl).
					Border(1, amberInk).Padding(2, 8).Shrink(0)
				chip.Children(func() {
					ui.Text(c, "Conflict").FontSize(sizeMonoSm).Bold().TextColor(amberInk)
				})
				resolve := ui.Button(c, "Resolve conflict").
					Tooltip("Compare versions and resolve this conflict")
				resolve.Padding(4, 8)
				if resolve.Clicked() {
					a.openResolveConflict()
				}
				if a.note.ConflictOriginalID != "" {
					if ui.Button(c, "View original").Tooltip("Inspect the original; return with " + historyHint(false)).Clicked() {
						id := a.note.ConflictOriginalID
						if original, err := a.svc.GetNote(id); err != nil {
							a.saveErr = err.Error()
						} else if original == nil {
							a.saveErr = "The original note is no longer on this device."
						} else {
							a.remember(a.note.ID)
							a.selectedNotebook, a.selectedTag, a.query = "", "", ""
							a.restoring = true
							a.reloadNotes()
							a.restoring = false
							a.showNote(id)
							a.remember(id)
						}
					}
				}
			})
		}
		// These actions may clear a.note. Build them after the last dereference.
		a.menu(c, move, &a.moveOpen, "move", 240, func() { a.moveMenu(c, t) })
		a.menu(c, tags, &a.tagMenuOpen, "note-tags", 240, func() { a.tagMenu(c, t) })
	})
}

// emptyEditor is the editor with no note open.
func (a *nativeApp) emptyEditor(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Padding(24).Children(func() {
		// The header already names the state, so the body offers the way out
		// rather than saying it twice.
		if len(a.notes) == 0 && a.query == "" && a.selectedNotebook == "" && a.selectedTag == "" {
			ui.Text(c, "Your notes start here.").FontSize(sizeBody).TextColor(inkNavy)
			ui.Text(c, "Use New note above to write. Notes save automatically on this device; set up sync when you’re ready to keep another copy.").
				FontSize(sizeUI).TextColor(inkSoft)
		} else {
			ui.Text(c, "Pick a note from the list, or use New note above.").FontSize(sizeUI).TextColor(inkSoft)
		}
		if primaryAction(c, "+ New note").Label("Create note").Clicked() {
			a.newNote()
		}
		kbdHint(c, shortcutN())
	})
}

// kbdHint prints a mono keyboard hint under an action.
func kbdHint(c *ui.Context, text string) {
	ui.Text(c, text).Font(fontMono).FontSize(sizeMonoSm).TextColor(inkSoft)
}

// shortcutN is the platform's New note chord.
func shortcutN() string {
	if runtime.GOOS == "darwin" {
		return "⌘N"
	}
	return "Ctrl+N"
}

// --- Menus ------------------------------------------------------------------

// menu is every transient surface that hangs off a control: a paper-raised
// panel with the popover shadow, fading and rising once as it opens, placed
// below its anchor.
func (a *nativeApp) menu(c *ui.Context, anchor ui.Element, open *bool, key string, width float32, fn func()) ui.Element {
	if !*open {
		// Still record the closed state, so the next open animates.
		a.surfaces.opening(key, false)
	}
	run := a.surfaces.opening(key, *open)
	return ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		panel.Padding(6).Gap(2).MinWidth(width)
		panelStyle(panel, width)
		panelIn(panel, key, run)
		fn()
	})
}

// tagActionsMenu is the Rename/Delete menu for the selected tag.
func (a *nativeApp) tagActionsMenu(c *ui.Context, t *ui.Theme) {
	if menuItem(c, "tag-action:rename", "Rename tag…", false) {
		a.tagActions = false
		a.openRenameTag()
	}
	if menuItem(c, "tag-action:delete", "Delete tag…", false) {
		a.tagActions = false
		a.confirmDeleteTag = true
	}
}

// notebookMenu is the Rename/Delete menu for the selected notebook.
func (a *nativeApp) notebookMenu(c *ui.Context, t *ui.Theme) {
	if menuItem(c, "notebook:rename", "Rename notebook…", false) {
		a.notebookActions = false
		a.openRenameNotebook()
	}
	if menuItem(c, "notebook:delete", "Delete notebook…", false) {
		a.notebookActions = false
		a.confirmDeleteNotebook = true
	}
}

// sortMenu sets the note-list order. The default is updated_time descending,
// as before; the choice is per-window state, not a saved setting.
func (a *nativeApp) sortMenu(c *ui.Context, t *ui.Theme) {
	options := []struct {
		label string
		value string
	}{
		{"Updated (newest)", ""},
		{"Updated (oldest)", string(store.SortUpdatedAsc)},
		{"Created (newest)", string(store.SortCreatedDesc)},
		{"Created (oldest)", string(store.SortCreatedAsc)},
		{"Title A-Z", string(store.SortTitleAsc)},
		{"Title Z-A", string(store.SortTitleDesc)},
	}
	label(c, "Sort by")
	for _, opt := range options {
		chosen := a.sort == opt.value
		if menuItem(c, "sort:"+opt.value, opt.label, chosen) {
			a.sortOpen = false
			if !chosen {
				a.sort = opt.value
				a.reloadNotes()
			}
		}
	}
}

// tagMenu toggles the tags of the open note. The menu stays open so several
// tags can be added in one go.
func (a *nativeApp) tagMenu(c *ui.Context, t *ui.Theme) {
	label(c, "Tags on this note")
	if len(a.tags) == 0 {
		ui.Text(c, "No tags yet").FontSize(sizeSmall).TextColor(inkSoft).Padding(4, 6)
	} else {
		if len(a.tags) > 1 {
			ui.TextInput(c.Key("tag-filter"), &a.tagFilter).
				Placeholder("Filter tags…").
				Label("Filter tags").
				Width(228)
		}
		needle := strings.ToLower(strings.TrimSpace(a.tagFilter))
		filtered := make([]*models.Tag, 0, len(a.tags))
		for _, tag := range a.tags {
			if needle == "" || strings.Contains(strings.ToLower(tag.Title), needle) {
				filtered = append(filtered, tag)
			}
		}
		if len(filtered) == 0 {
			ui.Text(c, "No matching tags").FontSize(sizeSmall).TextColor(inkSoft).Padding(4, 6)
		} else {
			height := float32(32 * len(filtered))
			if height > 240 {
				height = 240
			}
			ui.Scroll(c).Width(228).Height(height).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for _, tag := range filtered {
						on := a.hasNoteTag(tag.ID)
						this := tag
						row := menuRow(c, "note-tag:"+tag.ID, on)
						row.Label("Tag: " + this.Title)
						clicked := row.Clicked()
						row.Children(func() {
							ui.Text(c, this.Title).Grow(1).SingleLine()
							if on {
								ui.Text(c, "✓").TextColor(violet)
							}
						})
						if clicked {
							a.toggleTag(this.ID)
						}
					}
				})
			})
		}
	}
	ui.Divider(c)
	if menuItem(c, "note-tag:new", "New tag…", false) {
		a.openNewTagForNote()
	}
}

// notebookPicker is the scrollable "move to notebook" menu shared by the open
// note and the multi-row selection.
func (a *nativeApp) notebookPicker(c *ui.Context, t *ui.Theme, heading, current string, onPick func(string)) {
	label(c, heading)
	if len(a.notebooks) == 0 {
		ui.Text(c, "No notebooks").FontSize(sizeSmall).TextColor(inkSoft).Padding(4, 6)
		return
	}
	if len(a.notebooks) > 1 {
		ui.TextInput(c.Key("notebook-filter:"+heading), &a.moveFilter).
			Placeholder("Filter notebooks…").
			Label("Filter notebooks").
			Width(228)
	}
	needle := strings.ToLower(strings.TrimSpace(a.moveFilter))
	filtered := make([]*models.Folder, 0, len(a.notebooks))
	for _, nb := range a.notebooks {
		if needle == "" || strings.Contains(strings.ToLower(nb.Title), needle) {
			filtered = append(filtered, nb)
		}
	}
	if len(filtered) == 0 {
		ui.Text(c, "No matching notebooks").FontSize(sizeSmall).TextColor(inkSoft).Padding(4, 6)
		return
	}
	height := float32(32 * len(filtered))
	if height > 280 {
		height = 280
	}
	ui.Scroll(c).Width(228).Height(height).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			for _, nb := range filtered {
				chosen := current != "" && current == nb.ID
				row := menuRow(c, "move:"+nb.ID, chosen)
				row.Label("Move to " + nb.Title)
				clicked := row.Clicked()
				indent := a.notebookDepth[nb.ID]
				if needle != "" {
					indent = 0
				}
				row.PaddingStart(10 + float32(indent)*14)
				row.Children(func() {
					ui.Text(c, nb.Title).Grow(1).SingleLine()
					if chosen {
						ui.Text(c, "✓")
					}
				})
				if clicked {
					onPick(nb.ID)
				}
			}
		})
	})
}

// moveMenu lists the notebooks the open note can move to.
func (a *nativeApp) moveMenu(c *ui.Context, t *ui.Theme) {
	current := ""
	if a.note != nil {
		current = a.note.ParentID
	}
	a.notebookPicker(c, t, "Move to notebook", current, func(id string) {
		a.moveOpen = false
		a.moveTo(id)
	})
}

// bulkMoveMenu moves every selected note to one notebook.
func (a *nativeApp) bulkMoveMenu(c *ui.Context, t *ui.Theme) {
	a.notebookPicker(c, t, "Move selected notes", "", func(id string) {
		a.moveSelected(id)
	})
}

// menuItem is one row of a menu. It returns whether the row was clicked.
func menuItem(c *ui.Context, key, text string, chosen bool) bool {
	row := menuRow(c, key, chosen)
	clicked := row.Clicked()
	row.Children(func() {
		ui.Text(c, text).Grow(1).SingleLine()
		if chosen {
			ui.Text(c, "✓").TextColor(violet)
		}
	})
	return clicked
}

// commandPalette renders the Spotlight/Kero-style quick switcher and command
// palette modal (Cmd/Ctrl+P or clicking the ⌘K badge).
func (a *nativeApp) commandPalette(c *ui.Context, t *ui.Theme) {
	if !a.paletteOpen {
		return
	}
	type paletteEntry struct {
		key      string
		title    string
		subtitle string
		shortcut string
		icon     *ui.SVG
		run      func()
	}

	buildEntries := func() []paletteEntry {
		q := strings.ToLower(strings.TrimSpace(a.paletteQuery))
		var entries []paletteEntry
		match := func(text string) bool {
			return q == "" || strings.Contains(strings.ToLower(text), q)
		}

		filter := service.NoteFilter{Page: service.Page{Limit: nativePageSize}, Sort: a.sort}
		for {
			page, err := a.svc.ListNoteSummaries(filter)
			if err != nil {
				a.saveErr = err.Error()
				break
			}
			for _, n := range page.Notes {
				n := n
				title := noteRowTitle(n)
				nbTitle := "Notes"
				if nb := a.notebookByID(n.ParentID); nb != nil {
					nbTitle = nb.Title
				}
				if match(title + " " + nbTitle + " " + n.Excerpt) {
					entries = append(entries, paletteEntry{
						key:      "note:" + n.ID,
						title:    title,
						subtitle: nbTitle,
						icon:     iconNotes,
						run:      func() { a.openPaletteNote(n) },
					})
				}
				if len(entries) >= 12 {
					break
				}
			}
			if len(entries) >= 12 || !page.HasMore || page.NextCursor == "" {
				break
			}
			filter.Cursor = page.NextCursor
		}

		mod := "⌘"
		if runtime.GOOS != "darwin" {
			mod = "Ctrl+"
		}
		cmds := []paletteEntry{
			{
				key: "cmd:new-note", title: "New Note", subtitle: "Command", shortcut: mod + "N", icon: iconPlus,
				run: func() { a.paletteOpen = false; a.newNote() },
			},
			{
				key: "cmd:new-notebook", title: "New Notebook…", subtitle: "Command", shortcut: mod + "⇧N", icon: iconFolder,
				run: func() { a.paletteOpen = false; a.openNewNotebook() },
			},
			{
				key: "cmd:new-tag", title: "New Tag…", subtitle: "Command", shortcut: mod + "⇧T", icon: iconTag,
				run: func() { a.paletteOpen = false; a.openNewTag() },
			},
			{
				key: "cmd:toggle-edit", title: "Toggle Edit / Preview", subtitle: "Command", shortcut: mod + "E", icon: iconEdit,
				run: func() { a.paletteOpen = false; a.toggleEdit() },
			},
			{
				key: "cmd:toggle-sidebar", title: "Toggle Notebooks Sidebar", subtitle: "Command", shortcut: mod + "\\", icon: iconSidebar,
				run: func() { a.paletteOpen = false; a.railOpen = !a.railOpen },
			},
			{
				key: "cmd:toggle-dark", title: "Toggle Dark Mode", subtitle: "Appearance", icon: iconEye,
				run: func() {
					a.paletteOpen = false
					a.toggleDark()
				},
			},
			{
				key: "cmd:sync-now", title: "Sync Now", subtitle: "Sync", shortcut: mod + "R", icon: iconSync,
				run: func() { a.paletteOpen = false; a.syncNow() },
			},
			{
				key: "cmd:settings", title: "Open Settings…", subtitle: "Preferences", shortcut: mod + ",", icon: iconSettings,
				run: func() { a.paletteOpen = false; a.openSettings() },
			},
		}
		for _, cmd := range cmds {
			if match(cmd.title + " " + cmd.subtitle) {
				entries = append(entries, cmd)
			}
		}
		return entries
	}

	ui.Modal(c, &a.paletteOpen, func() {
		var entries []paletteEntry
		ui.Column(c).Width(480).Gap(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 8).
				Background(paperSunken).Border(1, hairlineStrong).Radius(radiusControl).Children(func() {
				ui.Icon(c, iconSearch).Size(15, 15).TextColor(inkSoft).Shrink(0)
				in := ui.TextInput(c.Key("palette-input"), &a.paletteQuery).
					Placeholder("Type a note title or command…").
					Label("Command palette search").
					Background(ui.Transparent).
					Padding(4, 2).
					Grow(1).
					AutoFocus()
				if a.paletteFocus {
					in.Focus()
					a.paletteFocus = false
				}
				if in.Changed() {
					a.paletteIndex = 0
				}
				entries = buildEntries()
				if a.paletteIndex >= len(entries) {
					a.paletteIndex = max(0, len(entries)-1)
				}
				if c.Shortcut(0, ui.KeyDown) && len(entries) > 0 {
					a.paletteIndex = min(a.paletteIndex+1, len(entries)-1)
				} else if c.Shortcut(0, ui.KeyUp) && len(entries) > 0 {
					a.paletteIndex = max(a.paletteIndex-1, 0)
				}
				if in.Submitted() && a.paletteIndex >= 0 && a.paletteIndex < len(entries) {
					entries[a.paletteIndex].run()
				}
			})

			ui.Scroll(c.Key("palette-results")).MaxHeight(320).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					if len(entries) == 0 {
						ui.Text(c, "No matching notes or commands").Padding(12, 10).FontSize(sizeSmall).TextColor(inkSoft)
						return
					}
					for idx, item := range entries {
						idx, item := idx, item
						chosen := idx == a.paletteIndex
						row := ui.Row(c.Key("palette:"+item.key)).AlignItems(ui.Center).Gap(8).
							Padding(7, 10).Radius(radiusControl).Cursor(ui.CursorPointer).
							Label(item.title)
						chosenPill(row, chosen, row.Hovered())
						if row.Hovered() {
							a.paletteIndex = idx
						}
						if row.Clicked() {
							item.run()
						}
						row.Children(func() {
							ui.Icon(c, item.icon).Size(14, 14).TextColor(inkSoft).Shrink(0)
							lbl := ui.Text(c, item.title).FontSize(sizeUI).TextColor(inkNavy).SingleLine().Grow(1).MinWidth(0)
							if chosen {
								lbl.Bold()
							}
							ui.Text(c, item.subtitle).FontSize(sizeSmall).TextColor(inkSoft).Shrink(0)
							if item.shortcut != "" {
								ui.Text(c, item.shortcut).Font(fontMono).FontFeatures("tnum").FontSize(sizeMonoSm).
									TextColor(navyText).Shrink(0)
							}
						})
					}
				})
			})
		})
	})
}
