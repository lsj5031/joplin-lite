// Package service is the typed API seam for the UI shell: the MyGO bound
// services (M4) wrap these methods for the webview. Pure Go over the store —
// core principle 4: the backend stays a headless, independently testable Go
// service and the shell is only a shell.
//
// Request/response types are JSON-serializable on purpose: this is the
// bindings contract the web prototype's mock layer (web/src/lib/mock-data.ts)
// swaps out in M4. Settings (sync target, master password, clipper token)
// are wired at M4 too — they need the runtime config and the sync engine,
// not just the store.
package service

import (
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// Service is the typed UI API over the local store.
type Service struct {
	db *store.DB
}

// New creates a Service over an open store.
func New(db *store.DB) *Service {
	return &Service{db: db}
}

// Page is the pagination request for list calls.
type Page struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// NoteFilter is the note-list request: a folder's notes, a search, or the
// conflict-notes view (Joplin's virtual Conflicts notebook). Precedence when
// several are set: Query, then ConflictOnly, then FolderID.
type NoteFilter struct {
	FolderID     string `json:"folder_id,omitempty"`
	Query        string `json:"query,omitempty"`
	ConflictOnly bool   `json:"conflict_only,omitempty"`
	Page         Page   `json:"page"`
}

// NotePage is a page of notes.
type NotePage struct {
	Notes   []*models.Note `json:"notes"`
	HasMore bool           `json:"has_more"`
}

// ListNotes lists notes per the filter (newest first).
func (s *Service) ListNotes(f NoteFilter) (NotePage, error) {
	var (
		notes   []*models.Note
		hasMore bool
		err     error
	)
	switch {
	case f.Query != "":
		notes, hasMore, err = s.db.SearchNotes(f.Query, f.Page.Limit, f.Page.Offset)
	case f.ConflictOnly:
		notes, hasMore, err = s.db.ListConflictNotes(f.Page.Limit, f.Page.Offset)
	case f.FolderID != "":
		notes, hasMore, err = s.db.ListNotesByFolder(f.FolderID, false, "", "", f.Page.Limit, f.Page.Offset)
	default:
		notes, hasMore, err = s.db.ListNotes("", "", false, f.Page.Limit, f.Page.Offset)
	}
	if err != nil {
		return NotePage{}, err
	}
	if notes == nil {
		notes = []*models.Note{}
	}
	return NotePage{Notes: notes, HasMore: hasMore}, nil
}

// Search is shorthand for ListNotes with a query.
func (s *Service) Search(query string, p Page) (NotePage, error) {
	return s.ListNotes(NoteFilter{Query: query, Page: p})
}

// ConflictNotes is shorthand for the conflict-notes view.
func (s *Service) ConflictNotes(p Page) (NotePage, error) {
	return s.ListNotes(NoteFilter{ConflictOnly: true, Page: p})
}

// GetNote returns a note (nil when missing).
func (s *Service) GetNote(id string) (*models.Note, error) {
	return s.db.GetNote(id)
}

// CreateNote creates a note (timestamps/ID are filled in when empty).
func (s *Service) CreateNote(n *models.Note) error {
	return s.db.CreateNote(n)
}

// UpdateNote updates a note (updated_time is stamped by the store).
func (s *Service) UpdateNote(n *models.Note) error {
	return s.db.UpdateNote(n)
}

// DeleteNote deletes a note; the deletion propagates on the next sync.
func (s *Service) DeleteNote(id string) error {
	return s.db.DeleteNote(id)
}
