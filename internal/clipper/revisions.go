package clipper

import "net/http"

// Revisions (note history) — part of Joplin's Data API route set
// (services/rest/routes/revisions.ts). joplin-lite does not persist
// revisions yet (the sync pass records them as synced without their
// content), so the collection is always empty and individual revisions are
// always "not found". The routes exist so the API matches Joplin's route
// set; persisting revisions is future work.

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	p := parsePagination(r)
	writePaginated(w, []map[string]interface{}{}, false, p.Fields)
}

func (s *Server) handleGetRevision(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleUpdateRevision(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleDeleteRevision(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}
