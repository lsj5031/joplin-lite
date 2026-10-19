package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
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
		a.watchSync()
	}
	// DESIGN.md's palette and type scale, applied to every widget.
	c.SetTheme(designTheme())
	t := c.Theme()

	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.newNote()
	}
	if c.Shortcut(ui.Cmd, ui.KeyK) || c.Shortcut(ui.Cmd, ui.KeyF) {
		a.focusSearch = true
		a.focusBody = false
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		a.flush()
	}
	if c.Shortcut(ui.Cmd, ui.KeyE) {
		a.toggleEdit()
	}
	a.historyShortcuts(c)
	a.tick()
	a.tickSync()

	a.notebookDialogs(c, t)
	a.tagDialogs(c, t)
	a.settingsDialog(c, t)
	a.savedSearchDialog(c, t)

	if a.resolveConflict {
		if confirmDialog(c, actionTheme, &a.resolveConflict, "Keep this version?",
			"Copies this version to the original, then removes the conflict. If the original is missing, keeps this note as a regular note. Errors leave the conflict intact.",
			"Keep this version") == 1 {
			a.keepConflictVersion()
		}
	}

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
	// The grid is sized explicitly — chrome top and bottom, work between
	// them — so a tall pane can never push the status strip off the window.
	strip := statusHeight
	if a.narrow {
		strip = narrowStatusHeight
	}
	body := height - strip
	if a.narrow {
		body -= topBarHeight
	}
	if body < 0 {
		body = 0
	}

	if a.narrow {
		a.topBar(c, t)
	}
	if a.saveErr != "" {
		ui.Text(c, a.statusText()).Height(36).Shrink(0).Padding(8, 14).
			TextColor(signalRed).Background(paperRaised).SingleLine().Tooltip(a.statusText())
		body = max(0, body-36)
	}
	ui.Row(c).Height(body).Shrink(0).AlignItems(ui.Stretch).Clip().Children(func() {
		switch {
		case !a.narrow:
			if a.railOpen {
				a.railPane(c, t, true)
			}
			ui.Column(c).Width(listWidth).Shrink(0).Background(paperSunken).Children(func() {
				a.topBar(c, t)
				a.notesPane(c, t)
			})
			a.editorPane(c, t)
		case a.narrowEditor:
			a.editorPane(c, t)
		default:
			a.notesPane(c, t)
		}
	})
	a.statusStrip(c, t)
	if a.narrow && a.railOpen {
		a.railOverlay(c, t)
	}
}

// --- Chrome -----------------------------------------------------------------

// topBar is the upper chrome bar: the wordmark, the search field that filters
// the note list, and the three controls that are always available.
func (a *nativeApp) topBar(c *ui.Context, t *ui.Theme) {
	if !a.narrow {
		ui.Column(c).Padding(24, 18, 12, 18).Gap(18).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				mark(c)
				ui.Text(c, "joplin-lite").Font(fontHeading).FontSize(25).Grow(1)
				if chromeButton(c, "⚙").Label("Settings").Clicked() {
					a.openSettings()
				}
			})
			ui.Row(c).Height(36).Children(func() { a.searchField(c, t) })
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if chromeButton(c, "+ New note").Label("New note").Clicked() {
					a.newNote()
				}
				ui.Spacer(c)
				if chromeButton(c, "Notebooks").Label("Open notebooks").Tooltip("Notebooks and tags").Clicked() {
					a.railOpen = !a.railOpen
				}
			})
		})
		return
	}
	ui.Row(c).Height(topBarHeight).Shrink(0).AlignItems(ui.Center).Gap(12).Padding(0, 14).
		Background(navyDeep).Children(func() {
		if a.narrow {
			btn := chromeButton(c, "Notebooks")
			btn.Label("Open notebooks")
			btn.Tooltip("Notebooks and tags")
			if btn.Clicked() {
				a.railOpen = !a.railOpen
			}
		}
		mark(c)
		ui.Column(c).Gap(0).Shrink(0).Children(func() {
			ui.Text(c, "joplin-lite").Font(fontHeading).FontSize(22).TextColor(inkNavy)
		})
		a.searchField(c, t)
		primary := primaryAction(c, "New note")
		if a.narrow {
			primary.Label("New note")
		}
		if primary.Clicked() {
			a.newNote()
		}
		settings := chromeButton(c, "")
		settings.Label("Settings").Tooltip("Sync target, master password and clipper token")
		settings.Padding(0, 8).MinWidth(34)
		settings.Children(func() { chromeGlyph(c, "⚙", 16) })
		if settings.Clicked() {
			a.openSettings()
		}
	})
}

// mark is the folded sheet beside the Humanist wordmark.
func mark(c *ui.Context) ui.Element {
	return ui.Box(c).Size(26, 32).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		x, y := r.X, r.Y
		var page, fold, crease ui.Path
		page.MoveTo(x+3, y+2).LineTo(x+18, y+2).LineTo(x+24, y+10).LineTo(x+24, y+30).LineTo(x+3, y+30).Close()
		fold.MoveTo(x+3, y+30).LineTo(x+18, y+12).LineTo(x+24, y+12).LineTo(x+14, y+30).Close()
		crease.MoveTo(x+18, y+2).LineTo(x+18, y+12).LineTo(x+24, y+12).Close()
		p.FillPath(&page, routeBlue.Alpha(0.25))
		p.FillPath(&fold, routeBlue.Alpha(0.75))
		p.FillPath(&crease, routeBlue)
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

// chromeGlyph is an icon-sized label on the chrome, never below the 24px its
// row leaves it.
func chromeGlyph(c *ui.Context, glyph string, size float32) ui.Element {
	return ui.Text(c, glyph).FontSize(size).TextColor(navyText).MinWidth(size)
}

// searchField is the compact paper search input, themed so
// its placeholder, caret and selection read on the chrome, with the mono kbd
// hint at its end.
func (a *nativeApp) searchField(c *ui.Context, t *ui.Theme) {
	field := designTheme()
	field.Text = inkNavy
	field.TextMuted = navyText
	field.Surface = navyDeep
	field.Accent = routeBlueSoft
	field.Selection = routeBlueSoft.Alpha(0.35)
	field.Border = navyLine

	ui.Box(c).Height(34).Grow(1).MaxWidth(420).Background(navyDeep).
		Border(1, navyLine).Radius(radiusControl).Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Center).Gap(8).Padding(0, 10).Children(func() {
			icon := ui.Box(c).Size(15, 15).Shrink(0)
			icon.Draw(func(p *ui.Painter, r ui.Rect) { magnifier(p, r, navyText) })
			underTheme(c, func() *ui.Theme { return field }, func() ui.Element {
				in := ui.TextInput(c.Key("search"), &a.query).
					Placeholder("Search all notes…").
					Label("Search notes").
					TextColor(inkNavy).
					SelectionColor(routeBlueSoft.Alpha(0.35)).
					Background(ui.Transparent).
					Padding(0).
					Grow(1)
				if a.focusSearch {
					in.Focus()
					a.focusSearch = false
				}
				if in.Changed() {
					a.reloadNotes()
					// A narrowed query can drop the open note from the rows,
					// and the reload then opens the first match. That note must
					// not take the keyboard: the user is typing here, and every
					// character after the first would land in its body.
					a.focusBody = false
				}
				return in
			})
			if a.query != "" {
				clear := chromeButton(c, "×")
				clear.Label("Clear search").Tooltip("Clear search")
				clear.Padding(0, 4).MinWidth(20)
				if clear.Clicked() {
					a.query = ""
					a.reloadNotes()
				}
			}
			kbd := ui.Row(c).Shrink(0).Height(20).Radius(radiusSmall).Border(1, navyLine).
				Padding(0, 6).AlignItems(ui.Center)
			kbd.Children(func() {
				label := "⌘K"
				if runtime.GOOS != "darwin" {
					label = "Ctrl+K"
				}
				ui.Text(c, label).Font(fontMono).FontSize(sizeMonoSm).TextColor(navyText)
			})
		})
	})
}

// statusStrip is the lower chrome bar: the sync line, what the last pass did,
// the counts a machine produced, and the sync controls.
func (a *nativeApp) statusStrip(c *ui.Context, t *ui.Theme) {
	st := a.syncState()
	notice := a.syncNotice()

	state := func() {
		dot := ui.Box(c).Size(7, 7).Radius(3.5).Shrink(0)
		dot.Background(statusDotColor(st, notice))
		ui.Text(c, notice.Line).FontSize(sizeSmall).TextColor(inkSoft)
		if notice.Hint != "" {
			ui.Text(c, notice.Hint).FontSize(sizeMonoSm).TextColor(navyText).SingleLine().MinWidth(0)
		}
	}
	controls := func() {
		if !st.Enabled {
			// Sync now has nothing to do on a vault without a target, so the
			// first button leads to the dialog that sets one up.
			if chromeButton(c, "Set up sync").Tooltip("Choose a sync target").Clicked() {
				a.openSettings()
			}
		} else if chromeButton(c, "Sync now").Disabled(st.Running || st.Pending).Clicked() {
			a.syncNow()
		}
		ui.Text(c, noteCountLabel(len(a.notes), a.hasMore)).Font(fontMono).FontSize(sizeMonoSm).
			TextColor(navyText)
		if !a.narrow {
			ui.Text(c, "·").Font(fontMono).FontSize(sizeMonoSm).TextColor(navyLine)
			ui.Text(c, notebookCountLabel(len(a.notebooks))).Font(fontMono).
				FontSize(sizeMonoSm).TextColor(navyText)
		}
	}

	ui.Row(c).Height(statusHeight).Shrink(0).AlignItems(ui.Center).Gap(12).Padding(0, 14).
		Background(navyDeep).Children(func() {
		ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(10).MinWidth(0).Children(func() {
			state()
			ui.Spacer(c)
			controls()
		})
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
func (a *nativeApp) railPane(c *ui.Context, t *ui.Theme, withHeader bool) {
	pane := ui.Column(c).Width(railWidth).Background(paperSunken)
	if a.narrow {
		pane.Grow(1)
	} else {
		pane.FillHeight().Shrink(0)
	}
	pane.Children(func() {
		if withHeader {
			ui.Row(c).Padding(12, 14, 8, 14).Gap(6).AlignItems(ui.Center).Children(func() {
				label(c, "Notebooks")
				ui.Text(c, fmt.Sprintf("%d", len(a.notebooks))).Font(fontMono).
					FontSize(sizeMonoSm).TextColor(inkSoft)
				ui.Spacer(c)
				add := ui.Button(c, "+").Label("New notebook").Tooltip("New notebook")
				add.Padding(2, 6).MinWidth(24)
				if add.Clicked() {
					a.openNewNotebook()
				}
				if a.notebookByID(a.selectedNotebook) != nil {
					actions := ui.Button(c, "…").Label("Notebook actions").
						Tooltip("Rename or delete this notebook")
					actions.Padding(2, 6).MinWidth(24)
					if actions.Clicked() {
						a.notebookActions = !a.notebookActions
					}
					a.menu(c, actions, &a.notebookActions, "notebook", 190, func() {
						a.notebookMenu(c, t)
					})
				}
				if !a.narrow && ui.Button(c, "×").Label("Close notebooks").Clicked() {
					a.railOpen = false
				}
			})
		}
		a.notebookRows(c, t)
		hairlineRule(c)
		ui.Row(c).Padding(8, 14).Gap(6).AlignItems(ui.Center).Children(func() {
			label(c, "Tags")
			ui.Spacer(c)
			add := ui.Button(c, "+").Label("New tag").Tooltip("New tag")
			add.Padding(2, 6).MinWidth(24)
			if add.Clicked() {
				a.openNewTag()
			}
			if a.selectedTag != "" {
				actions := ui.Button(c, "…").Label("Tag actions").Tooltip("Rename or delete this tag")
				actions.Padding(2, 6).MinWidth(24)
				if actions.Clicked() {
					a.tagActions = !a.tagActions
				}
				a.menu(c, actions, &a.tagActions, "tag-actions", 170, func() {
					a.tagActionsMenu(c, t)
				})
				clear := ui.Button(c, "×").Label("Clear tag").Tooltip("Clear the tag filter")
				clear.Padding(2, 6).MinWidth(24)
				if clear.Clicked() {
					a.selectedTag, a.selectedID = "", ""
					a.reloadNotes()
				}
			}
		})
		a.tagRows(c, t)
	})
}

// notebookRows is the notebook tree: one 32px row per notebook, indented by
// depth with a hairline spine, chosen in violet, dragging a note onto a row
// moving it there.
func (a *nativeApp) notebookRows(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c).Grow(1).Padding(4, 8).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			a.railRow(c, t, "all", "", "All notes", "", -1, 0)
			a.railRow(c, t, "conflicts", conflictsRail, "Conflicts", "", -1, 0)
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
	row := ui.Row(c.Key("rail:" + key)).Height(navRowHeight).MinHeight(navRowHeight).
		Radius(radiusControl).AlignItems(ui.Center).Gap(6).Shrink(0).
		PaddingStart(8 + float32(depth)*14).PaddingEnd(8)
	hover := row.Animate("hover", boolF(row.Hovered()), motionHover.Duration)
	switch {
	case chosen:
		row.Background(violet.Alpha(0.12)).Border(1, violet)
	case hover > 0.002:
		row.Background(paperSunken.Mix(paperRaised, 1-hover).Alpha(0.92 * hover))
	}
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
	row.Label(text)
	clicked := row.Clicked()
	row.Children(func() {
		if flag == "#" {
			ui.Text(c, "#").FontSize(sizeSmall).TextColor(inkSoft)
		}
		name := ui.Text(c, text).FontSize(sizeUI).SingleLine().Grow(1)
		if chosen {
			name.Bold()
		}
		if count > 0 {
			ui.Text(c, fmt.Sprintf("%d", count)).Font(fontMono).FontSize(sizeMonoSm).TextColor(inkSoft)
		}
	})
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
				rail := ui.Column(c).Width(railWidth).Shrink(0).Background(paperSunken).
					Translate((v-1)*railWidth, 0)
				rail.Children(func() {
					ui.Row(c).Padding(12, 14).Gap(6).AlignItems(ui.Center).Children(func() {
						label(c, "Notebooks")
						ui.Spacer(c)
						close := ui.Button(c, "×").Label("Close notebooks").Tooltip("Close")
						close.Padding(2, 6).MinWidth(24)
						if close.Clicked() {
							a.railOpen = false
						}
					})
					a.railPane(c, t, true)
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
	width := listWidth
	if a.narrow {
		width = 0
	}
	pane := ui.Column(c).Grow(1).MinHeight(0).Background(paperSunken)
	if width > 0 {
		pane.Width(width)
	} else {
		pane.Grow(1)
	}
	pane.Children(func() {
		ui.Row(c).Padding(10, 14, 6, 14).Gap(4).AlignItems(ui.Center).Children(func() {
			if a.narrow {
				if back := toolbarButton(c, "<", "Back to the note list"); back.Clicked() {
					a.narrowEditor = false
				}
			} else {
				back := toolbarButton(c, "<", historyHint(false))
				back.Label("Back")
				back.Disabled(!a.canBack())
				if back.Clicked() {
					a.goHistory(-1)
				}
				forward := toolbarButton(c, ">", historyHint(true))
				forward.Label("Forward")
				forward.Disabled(!a.canForward())
				if forward.Clicked() {
					a.goHistory(1)
				}
			}
			title := ui.Text(c, a.listTitle()).FontSize(sizeBody).Bold().Grow(1).SingleLine()
			_ = title
			ui.Text(c, fmtCount(len(a.notes), a.hasMore)).Font(fontMono).
				FontSize(sizeMonoSm).TextColor(inkSoft)
			sortButton := ui.Button(c, "Sort").Label("Sort notes").Tooltip("Sort the note list")
			sortButton.Padding(4, 6)
			if sortButton.Clicked() {
				a.sortOpen = !a.sortOpen
			}
			a.menu(c, sortButton, &a.sortOpen, "sort", 190, func() { a.sortMenu(c, t) })
			add := ui.Button(c, "+").Label("New note").Tooltip("New note (Cmd/Ctrl+N)")
			add.Padding(4, 6).MinWidth(26)
			if add.Clicked() {
				a.newNote()
			}
		})
		ui.Row(c).Padding(0, 14, 8, 14).Gap(6).AlignItems(ui.Center).Children(func() {
			saved := ui.Button(c, "Saved searches").Label("Saved searches").Tooltip("Saved searches")
			saved.Padding(4, 8).FontSize(sizeMonoSm)
			if saved.Clicked() {
				a.savedMenuOpen = !a.savedMenuOpen
			}
			a.menu(c, saved, &a.savedMenuOpen, "saved", 250, func() { a.savedSearchMenu(c, t) })
			if a.query != "" {
				chip := ui.Row(c).Gap(6).AlignItems(ui.Center).Radius(999).
					Border(1, hairlineStrong).Padding(2, 8).Shrink(0)
				chip.Children(func() {
					ui.Text(c, clip(a.query, 28)).FontSize(sizeMonoSm).TextColor(inkSoft)
					clear := ui.Button(c, "×").Label("Clear search")
					clear.Padding(0, 4).MinWidth(18)
					if clear.Clicked() {
						a.query = ""
						a.reloadNotes()
					}
				})
			}
		})
		if n := a.selection.Len(); n > 0 {
			ui.Row(c).Padding(2, 14, 6, 14).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("%d selected", n)).Font(fontMono).FontSize(sizeMonoSm).
					TextColor(violet).Grow(1)
				move := ui.Button(c, "Move…").Label("Move selected notes").
					Tooltip("Move the selected notes")
				move.Padding(4, 8)
				if move.Clicked() {
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
		hairlineRule(c)
		a.noteRows(c, t)
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
	a.list.Selected = &a.selectedNote
	a.list.Selection = &a.selection
	a.list.Key = func(i int) any { return a.notes[i].ID }
	a.list.Label = func(i int) string { return noteRowTitle(a.notes[i]) }
	// Dragging a chosen row drags the whole choice; an unchosen row drags
	// only itself. The drag starts on the row's grip: making the whole row
	// a drag source would take the press from the list, and a click would
	// stop choosing a row.
	dragIDs := a.selectedIDs()
	// The list chooses rows with the theme's accent, so it is built under the
	// row theme: a violet tint, not a solid fill (DESIGN.md's chosen row).
	list := underTheme(c, listTheme, func() ui.Element {
		return ui.List(c, &a.list, len(a.notes), func(i int) {
			a.noteRow(c, t, i, dragIDs)
		})
	}).Grow(1)
	if list.Changed() {
		if a.narrow && c.Modifiers()&(ui.Cmd|ui.Shift) == 0 {
			a.narrowEditor = true
		}
		a.loadNote()
		if a.narrow && !a.narrowEditor {
			a.focusBody = false
		}
	}
	if a.hasMore {
		ui.Row(c).Padding(6, 14).Children(func() {
			more := ui.Button(c, "Load more").Label("Load more notes")
			more.Padding(4, 8)
			if more.Clicked() {
				a.loadMore()
			}
		})
	}
}

// noteRow draws one row of the list: ~40px, hairline ruled, a flag column, a
// title and meta stack, the updated time in mono at its end — and, for the
// note the editor follows, the violet tint and 1px violet edge that mark a
// choice (MyGO's own List paints only its multi-selection, so the current row
// is drawn here).
func (a *nativeApp) noteRow(c *ui.Context, t *ui.Theme, i int, dragIDs []string) {
	n := a.notes[i]
	title := noteRowTitle(n)
	current := i == a.selectedNote
	bulk := a.selection.Has(n.ID)
	row := ui.Row(c).Padding(4, 14).Gap(8).AlignItems(ui.Center).MinHeight(rowHeight).Key(n.ID)
	hover := row.Animate("hover", boolF(row.Hovered()), motionHover.Duration)
	switch {
	case current:
		row.Background(violet.Alpha(0.13 + 0.05*hover))
	case bulk:
		row.Background(violet.Alpha(0.07 + 0.04*hover))
	case hover > 0.002:
		row.Background(paperSunken.Alpha(0.92 * hover))
	}
	row.DrawOver(func(p *ui.Painter, r ui.Rect) {
		// The rule between rows (the Hairline Rule), then the chosen row's
		// violet edge over it.
		p.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, hairline)
		if current {
			p.Line(r.X, r.Y, r.X, r.Y+r.H, 2, violet)
		}
	})

	// The flag column keeps the titles of every row aligned.
	flags := ui.Box(c).Width(14).Shrink(0).AlignItems(ui.Center)
	flags.Children(func() {
		switch {
		case n.IsConflict != 0:
			flag := ui.Text(c, "!").FontSize(sizeSmall).Bold().TextColor(signalRed).Label("Conflict")
			_ = flag
		case n.IsTodo != 0 && n.TodoCompleted != 0:
			ui.Text(c, "☑").FontSize(sizeSmall).TextColor(moss).Label("Completed")
		case n.IsTodo != 0:
			ui.Text(c, "☐").FontSize(sizeSmall).TextColor(inkSoft).Label("To-do")
		}
	})

	// The grip appears as the pointer rests on the row, so the list reads as
	// a list until it is a drag.
	grip := ui.Text(c, "⣿").FontSize(sizeMonoSm).TextColor(hairlineStrong)
	grip.Label("Drag note " + title).Tooltip("Drag to a notebook")
	if a.selection.Has(n.ID) && len(dragIDs) > 0 {
		grip.Drag(dragIDs)
	} else {
		grip.Drag([]string{n.ID})
	}
	// The grip is the one thing a row never shows at rest: it comes with the
	// hover that says the row can be picked up.
	opacity := float32(0)
	if hover > 0.5 {
		opacity = 0.15 + 0.85*hover
	}
	grip.Opacity(opacity)

	row.Children(func() {
		ui.Column(c).Gap(0).Grow(1).MinWidth(0).Children(func() {
			name := ui.Text(c, title).FontSize(sizeUI).LineHeight(1.25).SingleLine()
			if current {
				name.FontWeight(600)
			}
			meta := n.Excerpt
			if meta == "" {
				meta = "Empty note"
			}
			ui.Text(c, excerpt(meta)).FontSize(sizeLabel).LineHeight(1.25).
				TextColor(inkSoft).SingleLine()
		})
		ui.Text(c, time.UnixMilli(n.UpdatedTime).Format("02 Jan")).
			Font(fontMono).FontSize(sizeMonoSm).TextColor(inkSoft).Shrink(0)
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
	ui.Column(c).Grow(1).MinWidth(0).Background(paperRaised).Children(func() {
		a.editorHeader(c, t)
		hairlineRule(c)
		if a.note == nil {
			a.emptyEditor(c, t)
			return
		}
		if a.editing {
			// Keep the writing surface quiet; code remains monospace in preview.
			editor := ui.TextArea(c.Key("body:"+a.note.ID), &a.body).
				Placeholder("Write in Markdown…").
				Label("Note body").
				Font(fontSans).
				FontSize(sizeBody).
				LineHeight(1.9).
				Padding(32, 48).
				Background(paperRaised).
				Border(0, ui.Transparent).
				FillWidth().
				Grow(1)
			if a.focusBody {
				editor.AutoFocus()
				a.focusBody = false
			}
			if editor.Changed() {
				a.markDirty()
			}
			return
		}
		ui.Scroll(c).Grow(1).Padding(48, 48).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MaxWidth(700).Gap(20).Children(func() {
					if a.body == "" {
						ui.Text(c, "This note is empty.").FontSize(sizeBody).TextColor(inkSoft)
						return
					}
					a.md.Theme = &markdown.Theme{Font: fontSans, FontSize: sizeBody, LineHeight: 1.9, CodeFont: fontMono}
					markdown.View(c, a.body, a.md)
				})
			})
		})
	})
}

// editorHeader is the note's own chrome: the title field and the mode at the
// top, the note's state and its actions beneath.
func (a *nativeApp) editorHeader(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Shrink(0).Children(func() {
		ui.Row(c).Padding(12, contentPad, 6, contentPad).Gap(10).AlignItems(ui.Center).Children(func() {
			if a.narrow {
				back := toolbarButton(c, "<", "Back to the note list")
				back.Label("Back to notes")
				if back.Clicked() {
					a.narrowEditor = false
					a.selectedNote = -1
				}
			}
			if a.note == nil {
				ui.Text(c, "No note selected").FontSize(sizeTitle).Bold().Grow(1)
				return
			}
			// A borderless title field (DESIGN.md's editor title): the text
			// sits on the surface, the caret is the only ornament.
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
			todo := ui.Checkbox(c, &a.todo, "Todo")
			if todo.Changed() {
				a.setTodo(a.todo)
			}
			mode := ui.Button(c, a.modeLabel()).Tooltip("Edit/Preview (Cmd/Ctrl+E)")
			mode.Padding(4, 10)
			if mode.Clicked() {
				a.toggleEdit()
			}
		})
		if a.note == nil {
			return
		}
		ui.Row(c).Padding(0, contentPad, 8, contentPad).Gap(6).AlignItems(ui.Center).Children(func() {
			meta := a.noteMeta()
			ui.Text(c, meta).Font(fontMono).FontSize(sizeMonoSm).TextColor(inkSoft).SingleLine().MinWidth(0).Grow(1)
		})
		if len(a.noteTags) > 0 {
			ui.Row(c).Padding(0, contentPad, 8, contentPad).Children(func() {
				names := make([]string, 0, len(a.noteTags))
				for _, tag := range a.noteTags {
					names = append(names, tag.Title)
				}
				ui.Text(c, strings.Join(names, " · ")).FontSize(sizeMonoSm).TextColor(inkSoft).
					SingleLine().MinWidth(0).Grow(1).Tooltip(strings.Join(names, ", "))
			})
		}
		if a.note.IsConflict != 0 {
			ui.Row(c).Padding(0, contentPad, 8, contentPad).Gap(6).AlignItems(ui.Center).Children(func() {
				chip := ui.Row(c).Gap(4).AlignItems(ui.Center).Radius(radiusControl).
					Border(1, amberInk).Padding(2, 8).Shrink(0)
				chip.Children(func() {
					ui.Text(c, "Conflict").FontSize(sizeMonoSm).Bold().TextColor(amberInk)
				})
				resolve := ui.Button(c, "Resolve conflict").
					Tooltip("Choose how to keep this version")
				resolve.Padding(4, 8)
				if resolve.Clicked() {
					a.resolveConflict = true
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
		ui.Row(c).Padding(0, contentPad, 8, contentPad).Gap(6).AlignItems(ui.Center).Children(func() {
			tags := ui.Button(c, "Tags").Label("Note tags").Tooltip("Tags on this note")
			tags.Padding(4, 8)
			if tags.Clicked() {
				a.tagMenuOpen = !a.tagMenuOpen
			}
			a.menu(c, tags, &a.tagMenuOpen, "note-tags", 240, func() { a.tagMenu(c, t) })

			move := ui.Button(c, "Move").Label("Move note").Tooltip("Move to another notebook")
			move.Padding(4, 8)
			if move.Clicked() {
				a.moveOpen = !a.moveOpen
			}
			a.menu(c, move, &a.moveOpen, "move", 240, func() { a.moveMenu(c, t) })

			del := ui.Button(c, "Delete…").Label("Delete note").Tooltip("Delete note")
			del.Padding(4, 8)
			if del.Clicked() {
				a.confirmDelete = true
			}
		})
	})
}

// noteMeta is the machine-made line under the title: the notebook, the updated
// time and the size of the note, all in mono with tabular figures.
func (a *nativeApp) noteMeta() string {
	parts := []string{}
	if a.note != nil {
		if nb := a.notebookByID(a.note.ParentID); nb != nil {
			parts = append(parts, nb.Title)
		} else {
			parts = append(parts, "Unfiled")
		}
		parts = append(parts, time.UnixMilli(a.note.UpdatedTime).Format("02 Jan 2006 15:04"))
		words := len(strings.Fields(a.body))
		parts = append(parts, fmt.Sprintf("%d words", words))
	}
	return strings.Join(parts, "  ·  ")
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
		return
	}
	height := float32(26 * len(a.tags))
	if height > 260 {
		height = 260
	}
	ui.Scroll(c).Width(232).Height(height).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			for _, tag := range a.tags {
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

// notebookPicker is the scrollable "move to notebook" menu shared by the open
// note and the multi-row selection.
func (a *nativeApp) notebookPicker(c *ui.Context, t *ui.Theme, heading, current string, onPick func(string)) {
	height := float32(28 + 30*len(a.notebooks))
	if height > 320 {
		height = 320
	}
	label(c, heading)
	if len(a.notebooks) == 0 {
		ui.Text(c, "No notebooks").FontSize(sizeSmall).TextColor(inkSoft).Padding(4, 6)
		return
	}
	ui.Scroll(c).Width(248).Height(height).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			for _, nb := range a.notebooks {
				chosen := current != "" && current == nb.ID
				row := menuRow(c, "move:"+nb.ID, chosen)
				row.Label("Move to " + nb.Title)
				clicked := row.Clicked()
				indent := a.notebookDepth[nb.ID]
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
