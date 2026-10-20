package main

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
)

// settingSavedSearches holds the saved searches as JSON in the vault's local
// key_values table: they belong to this device (like the other desktop
// settings) and are not part of the synced note data.
const settingSavedSearches = "desktop.saved_searches"

type savedSearch struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

// loadSavedSearches reads the list. A malformed value is ignored rather than
// blocking startup; the next save overwrites it.
func (a *nativeApp) loadSavedSearches() {
	raw, err := a.svc.Setting(settingSavedSearches)
	if err != nil || raw == "" {
		return
	}
	var list []savedSearch
	if json.Unmarshal([]byte(raw), &list) != nil {
		return
	}
	a.savedSearches = list
}

func (a *nativeApp) persistSavedSearches(list []savedSearch) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if err := a.svc.SetSetting(settingSavedSearches, string(raw)); err != nil {
		return err
	}
	a.savedSearches = list
	return nil
}

// openSaveSearch starts the name dialog for the current query.
func (a *nativeApp) openSaveSearch() {
	if strings.TrimSpace(a.query) == "" {
		return
	}
	a.savedMenuOpen = false
	a.savedName = ""
	a.savedErr = ""
	a.savedOpen = true
}

// submitSavedSearch stores the query under the given name, replacing an
// existing entry with the same name (case-insensitively).
func (a *nativeApp) submitSavedSearch() {
	name := strings.TrimSpace(a.savedName)
	query := strings.TrimSpace(a.query)
	switch {
	case name == "":
		a.savedErr = "Give the search a name."
		return
	case query == "":
		a.savedErr = "Search for something first."
		return
	}

	next := make([]savedSearch, 0, len(a.savedSearches)+1)
	replaced := false
	for _, s := range a.savedSearches {
		if strings.EqualFold(s.Name, name) {
			s.Query = query
			replaced = true
		}
		next = append(next, s)
	}
	if !replaced {
		next = append(next, savedSearch{Name: name, Query: query})
	}
	if err := a.persistSavedSearches(next); err != nil {
		a.savedErr = err.Error()
		return
	}
	a.savedOpen = false
	a.savedErr = ""
}

// applySavedSearch runs a saved query in the current filter context.
func (a *nativeApp) applySavedSearch(s savedSearch) {
	a.savedMenuOpen = false
	a.query = s.Query
	a.reloadNotes()
}

// deleteSavedSearch removes an entry and persists the rest. A failed write
// leaves the list untouched.
func (a *nativeApp) deleteSavedSearch(name string) {
	next := make([]savedSearch, 0, len(a.savedSearches))
	for _, s := range a.savedSearches {
		if !strings.EqualFold(s.Name, name) {
			next = append(next, s)
		}
	}
	if err := a.persistSavedSearches(next); err != nil {
		a.saveErr = err.Error()
	}
}

// savedSearchDialog asks for the name to save the active query under.
func (a *nativeApp) savedSearchDialog(c *ui.Context, t *ui.Theme) {
	if a.savedDelete != "" {
		open := true
		if confirmDialog(c, dangerTheme, &open, "Delete saved search?",
			"Remove “"+a.savedDelete+"” from this device? Your notes are not deleted.", "Delete saved search") == 1 {
			a.deleteSavedSearch(a.savedDelete)
		}
		if !open {
			a.savedDelete = ""
		}
	}
	if !a.savedOpen {
		return
	}
	ui.Modal(c, &a.savedOpen, func() {
		ui.Text(c, "Save this search").FontSize(17).Bold()
		ui.Text(c, "Saving \""+clip(a.query, 60)+"\"").FontSize(12).TextColor(t.TextMuted)
		input := ui.TextInput(c.Key("saved-search-name"), &a.savedName).
			Placeholder("Name, e.g. Work TODOs").
			Label("Saved search name").
			Width(280).
			AutoFocus()
		replacing := false
		for _, s := range a.savedSearches {
			if strings.EqualFold(s.Name, strings.TrimSpace(a.savedName)) {
				replacing = true
				ui.Text(c, "This replaces “"+s.Name+"” ("+clip(s.Query, 60)+").").MaxWidth(280).FontSize(12).TextColor(amberInk)
				break
			}
		}
		if input.Submitted() && !replacing {
			a.submitSavedSearch()
		}
		if a.savedErr != "" {
			ui.Text(c, a.savedErr).FontSize(12).TextColor(t.Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				a.savedOpen = false
			}
			action := "Save"
			if replacing {
				action = "Replace saved search"
			}
			if primaryAction(c, action).Clicked() {
				a.submitSavedSearch()
			}
		})
	})
}

// savedSearchMenu lists the saved searches; each row applies the query and
// has its own delete action, so a row is never both.
func (a *nativeApp) savedSearchMenu(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(238).Gap(2).Children(func() {
		ui.Text(c, "Saved searches").FontSize(11).TextColor(t.TextMuted)
		if len(a.savedSearches) == 0 {
			ui.Text(c, "No saved searches yet").FontSize(12).TextColor(t.TextMuted)
		} else {
			height := min(float32(len(a.savedSearches))*40, float32(240))
			ui.Scroll(c).Height(height).Shrink(0).Children(func() {
				ui.Column(c).Children(func() {
					for _, s := range a.savedSearches {
						ui.Row(c.Key("saved:" + s.Name)).Padding(2).Gap(4).AlignItems(ui.Center).Children(func() {
							apply := ui.Row(c.Key("saved-apply:"+s.Name)).
								Label("Apply saved search "+s.Name).
								Padding(6, 8).Radius(5).Grow(1)
							if apply.Hovered() {
								apply.Background(t.Accent).TextColor(t.AccentText)
							}
							if apply.Clicked() {
								a.applySavedSearch(s)
							}
							apply.Children(func() {
								ui.Text(c, s.Name).Grow(1).MinWidth(0).SingleLine().Tooltip(s.Query)
							})
							if iconButton(c, "delete-saved-search:"+s.Name, iconClose, "Delete saved search "+s.Name, "Delete saved search").Clicked() {
								a.savedMenuOpen = false
								a.savedDelete = s.Name
							}
						})
					}
				})
			})
		}
		ui.Divider(c)
		if strings.TrimSpace(a.query) == "" {
			ui.Text(c, "Search first to save it").FontSize(12).TextColor(t.TextMuted)
			return
		}
		entry := ui.Row(c.Key("save-current-search")).
			Label("Save current search").
			Padding(6, 8).Radius(5)
		if entry.Hovered() {
			entry.Background(t.Accent).TextColor(t.AccentText)
		}
		if entry.Clicked() {
			a.openSaveSearch()
		}
		entry.Children(func() { ui.Text(c, "Save current search…") })
	})
}
