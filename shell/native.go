package main

import (
	"fmt"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
	"github.com/lsj5031/joplin-lite/service"
)

// nativeListLimit is how many rows the spike loads. Real paging comes in
// Phase 1; this is enough to compare the two shells on the same snapshot.
const nativeListLimit = 500

// nativeApp is the Phase 0 native-UI spike: notebooks, a note list and the
// rendered Markdown of the selected note, drawn by MyGO with no webview.
// Read-only on purpose — the point is the memory measurement and a working
// data path from store to native widgets.
type nativeApp struct {
	svc *service.Service

	notebooks []*models.Folder
	notes     []*models.NoteSummary

	selectedNotebook string // "" is All notes
	selectedNote     int
	list             ui.ListState

	body   string
	bodyID string

	md markdown.Options
}

func newNativeApp(db *store.DB) *nativeApp {
	app := &nativeApp{svc: service.New(db), selectedNote: -1}
	app.reloadNotebooks()
	app.reloadNotes()
	return app
}

func (a *nativeApp) reloadNotebooks() {
	page, err := a.svc.ListNotebooks(service.Page{Limit: nativeListLimit})
	if err != nil {
		return
	}
	a.notebooks = page.Notebooks
}

func (a *nativeApp) reloadNotes() {
	filter := service.NoteFilter{Page: service.Page{Limit: nativeListLimit}}
	if a.selectedNotebook != "" {
		filter.FolderID = a.selectedNotebook
	}
	page, err := a.svc.ListNoteSummaries(filter)
	if err != nil {
		return
	}
	a.notes = page.Notes
	if a.selectedNote < 0 || a.selectedNote >= len(a.notes) {
		a.selectedNote = 0
	}
	a.bodyID = ""
	a.loadBody()
}

func (a *nativeApp) loadBody() {
	if a.selectedNote < 0 || a.selectedNote >= len(a.notes) {
		a.body, a.bodyID = "", ""
		return
	}
	id := a.notes[a.selectedNote].ID
	if id == a.bodyID {
		return
	}
	note, err := a.svc.GetNote(id)
	if err != nil || note == nil {
		return
	}
	a.body, a.bodyID = note.Body, id
}

func (a *nativeApp) currentTitle() string {
	if a.selectedNote < 0 || a.selectedNote >= len(a.notes) {
		return ""
	}
	return a.notes[a.selectedNote].Title
}

func (a *nativeApp) view(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.notebooksPane(c, t)
		a.notesPane(c, t)
		a.previewPane(c, t)
	})
}

func (a *nativeApp) notebooksPane(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(220).Shrink(0).Gap(2).Background(t.Surface).Children(func() {
		ui.Row(c).Padding(10, 14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Notebooks").Bold().Grow(1)
			ui.Textf(c, "%d", len(a.notebooks)).FontSize(11).TextColor(t.TextMuted)
		})
		sidebar := ui.Sidebar(c, &a.selectedNotebook, func() {
			ui.SidebarItem(c.Key("all"), "all", nil, fmt.Sprintf("All notes (%d)", len(a.notes)))
			for _, nb := range a.notebooks {
				ui.SidebarItem(c.Key(nb.ID), nb.ID, nil, nb.Title)
			}
		}).Grow(1).Label("Notebooks")
		sidebar.OnChange(func() { a.reloadNotes() })
	})
}

func (a *nativeApp) notesPane(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(320).Shrink(0).Children(func() {
		ui.Row(c).Padding(10, 14).AlignItems(ui.Center).Children(func() {
			title := "All notes"
			if a.selectedNotebook != "" {
				for _, nb := range a.notebooks {
					if nb.ID == a.selectedNotebook {
						title = nb.Title
						break
					}
				}
			}
			ui.Text(c, title).Bold().Grow(1)
			ui.Textf(c, "%d", len(a.notes)).FontSize(11).TextColor(t.TextMuted)
		})
		ui.Divider(c)

		a.list.Selected = &a.selectedNote
		a.list.Key = func(i int) any { return a.notes[i].ID }
		a.list.Label = func(i int) string { return a.notes[i].Title }
		list := ui.List(c, &a.list, len(a.notes), func(i int) {
			n := a.notes[i]
			ui.Column(c).Padding(8, 14).Gap(2).Children(func() {
				ui.Text(c, n.Title).FontSize(13)
				if n.Excerpt != "" {
					ui.Text(c, n.Excerpt).FontSize(11).TextColor(t.TextMuted)
				}
				ui.Text(c, time.UnixMilli(n.UpdatedTime).Format("02 Jan 15:04")).
					FontSize(10).TextColor(t.TextMuted)
			})
		}).Grow(1)
		if list.Changed() {
			a.loadBody()
		}
	})
}

func (a *nativeApp) previewPane(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Row(c).Padding(10, 20).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.currentTitle()).Bold().FontSize(15).Grow(1)
		})
		ui.Divider(c)
		ui.Scroll(c).Grow(1).Padding(10, 20).Children(func() {
			if a.body == "" {
				ui.Text(c, "Select a note").TextColor(t.TextMuted)
				return
			}
			markdown.View(c, a.body, &a.md)
		})
	})
}

// openNativeWindow opens the spike window. It returns the window so the
// caller keeps the same lifecycle handling as the webview shell.
func openNativeWindow(be *backend) *mygo.Window {
	app := newNativeApp(be.db)
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     "Joplin Lite (native)",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		StateKey:  "native-main",
		Content:   ui.View(app.view),
	})
	if *smoke {
		// Native content has no page to evaluate, so the smoke check captures
		// the rendered window instead and quits.
		go func() {
			time.Sleep(*smokeIn)
			if png, err := win.CapturePage(); err == nil {
				_ = os.WriteFile("tmp/native-smoke.png", png, 0o644)
			}
			mygo.App.Quit()
		}()
	}
	return win
}
