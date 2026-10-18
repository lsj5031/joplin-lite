package clipper

import (
	"net/http"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter is required")
		return
	}

	searchType := r.URL.Query().Get("type")
	p := parsePagination(r)

	switch searchType {
	case "folder":
		folders, hasMore, err := s.db.SearchFolders(query, p.Limit, p.offset())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if folders == nil {
			folders = []*models.Folder{}
		}
		writePaginated(w, folders, hasMore, p.Fields)

	case "tag":
		tags, hasMore, err := s.db.SearchTags(query, p.Limit, p.offset())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if tags == nil {
			tags = []*models.Tag{}
		}
		writePaginated(w, tags, hasMore, p.Fields)

	default: // "note" or empty defaults to note search
		if p.Summary {
			sums, hasMore, next, err := s.db.ListNoteSummaries(store.NoteQuery{Query: query, IncludeConflicts: r.URL.Query().Get("include_conflicts") == "1"}, p.Limit, p.Cursor)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeCursorPage(w, sums, hasMore, next)
			return
		}

		var notes []*models.Note
		var hasMore bool
		var err error

		notes, hasMore, err = s.db.SearchNotesFiltered(query, p.Limit, p.offset(), r.URL.Query().Get("include_conflicts") == "1")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if notes == nil {
			notes = []*models.Note{}
		}
		writePaginated(w, notes, hasMore, p.Fields)
	}
}
