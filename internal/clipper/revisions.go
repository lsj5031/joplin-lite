package clipper

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lsj5031/joplin-lite/internal/models"
)

// Revisions (note history) — Joplin's Data API route set
// (services/rest/routes/revisions.ts, defaultAction over the revisions
// table). joplin-lite keeps revision items raw (item_raw, pushed back
// byte-for-byte) with a small index table; the API entity is the raw item's
// metadata — the diff fields (title_diff, body_diff, metadata_diff) live
// there, typed per the serialization rules.

// revisionEntity renders one revision as its API entity: the raw item's
// metadata keys verbatim (Joplin's RevisionEntity field names), over the
// index row as the baseline.
func (s *Server) revisionEntity(r *models.Revision) map[string]interface{} {
	entity := map[string]interface{}{
		"id":                r.ID,
		"parent_id":         r.ParentID,
		"item_id":           r.ItemID,
		"item_type":         r.ItemType,
		"item_updated_time": r.ItemUpdatedTime,
		"created_time":      r.CreatedTime,
		"updated_time":      r.UpdatedTime,
		"type_":             models.TypeRevision,
	}
	raw, _, ok, err := s.db.GetItemRaw(r.ID)
	if err != nil || !ok {
		return entity
	}
	_, fields := models.Deserialize(string(raw))
	for k, v := range fields {
		entity[k] = models.TypedMetaValue(k, v)
	}
	return entity
}

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	p := parsePagination(r)
	revs, err := s.db.ListRevisions(p.Limit, p.offset())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]map[string]interface{}, 0, len(revs))
	for _, rv := range revs {
		items = append(items, s.revisionEntity(rv))
	}
	total, err := s.db.CountRevisions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writePaginated(w, items, p.offset()+len(revs) < total, p.Fields)
}

func (s *Server) handleGetRevision(w http.ResponseWriter, r *http.Request) {
	rev, err := s.db.GetRevision(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rev == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	p := parsePagination(r)
	filterSingleItem(w, s.revisionEntity(rev), p.Fields)
}

// handleDeleteRevision deletes a note-history entry (and its remote copy on
// the next sync — revision deletion propagates like every other item).
func (s *Server) handleDeleteRevision(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rev, err := s.db.GetRevision(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rev == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.db.DeleteRevision(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id})
}

// handleUpdateRevision refuses rewrites: revisions are stored raw and pushed
// back byte-for-byte (Joplin's PUT would edit fields we must not mangle).
func (s *Server) handleUpdateRevision(w http.ResponseWriter, r *http.Request) {
	rev, err := s.db.GetRevision(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rev == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusBadRequest, "revisions cannot be modified")
}
