package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lsj5031/joplin-lite/internal/store"
)

func noteVersion(noteID, parentID, title, body string, updated int64) string {
	h := sha256.New()
	h.Write([]byte(noteID))
	h.Write([]byte{0})
	h.Write([]byte(parentID))
	h.Write([]byte{0})
	h.Write([]byte(title))
	h.Write([]byte{0})
	fmt.Fprintf(h, "%d", updated)
	h.Write([]byte{0})
	h.Write([]byte(body))
	return hex.EncodeToString(h.Sum(nil))
}

type searchCursor struct {
	hash   string
	offset int
}

func cursorFor(query, tag string) string {
	sum := sha256.Sum256([]byte(query + "\x00" + tag))
	return hex.EncodeToString(sum[:8])
}

func encodeCursor(hash string, offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("v1:%s:%d", hash, offset)))
}

func decodeCursor(raw, hash string) (int, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, false
	}
	parts := strings.SplitN(string(decoded), ":", 3)
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != hash {
		return 0, false
	}
	var offset int
	if _, err := fmt.Sscanf(parts[2], "%d", &offset); err != nil || offset < 0 {
		return 0, false
	}
	return offset, true
}

// searchNotes is the scoped retrieval broker. Search results are candidates,
// never evidence; only readNote registers a citation source.
func (m *Manager) searchNotes(ctx context.Context, run *Run, input SearchInput) (SearchPage, error) {
	if err := run.budget.noteToolCall(); err != nil {
		return SearchPage{}, err
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return SearchPage{}, toolErr("invalid_request", "The search query is empty.")
	}
	if len([]rune(query)) > 400 {
		return SearchPage{}, toolErr("invalid_request", "The search query is longer than 400 characters.")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	if limit > run.limits.SearchLimit {
		limit = run.limits.SearchLimit
	}
	hash := cursorFor(query, input.TagID)
	offset := 0
	if input.Cursor != "" {
		if next, ok := decodeCursor(input.Cursor, hash); ok {
			offset = next
		}
	}
	key := hash
	run.searchMu.Lock()
	hits := run.search[key]
	run.searchMu.Unlock()
	if hits == nil {
		found, err := m.vault.AssistantSearch(ctx, store.AssistantSearchQuery{
			Query:  query,
			TagID:  input.TagID,
			Scope:  store.AssistantScope{Kind: run.Scope.Kind, NoteID: run.Scope.NoteID, NotebookID: run.Scope.NotebookID, IncludeDescendants: run.Scope.IncludeDescendants},
			Limit:  run.limits.SearchCandidates,
			Offset: 0,
		})
		if err != nil {
			return SearchPage{}, toolErr("storage_failed", "The note search could not run.")
		}
		hits = found
		run.searchMu.Lock()
		if run.search == nil {
			run.search = make(map[string][]store.AssistantSearchHit)
		}
		run.search[key] = hits
		run.searchMu.Unlock()
	}
	page := SearchPage{Query: query, Evidence: "Search snippets are candidates only. Read a note with read_note before citing it."}
	if offset < len(hits) {
		end := offset + limit
		if end > len(hits) {
			end = len(hits)
		}
		for _, hit := range hits[offset:end] {
			page.Results = append(page.Results, SearchResult{
				NoteID:      hit.NoteID,
				Title:       hit.Title,
				Notebook:    hit.NotebookTitle,
				UpdatedTime: hit.UpdatedTime,
				Snippet:     hit.Snippet,
				Score:       hit.Score,
			})
		}
		if end < len(hits) {
			page.NextCursor = encodeCursor(hash, end)
		}
	}
	if page.Results == nil {
		page.Results = []SearchResult{}
	}
	return page, nil
}

func (m *Manager) readNote(ctx context.Context, run *Run, input ReadInput) (NotePassage, error) {
	if err := run.budget.noteToolCall(); err != nil {
		return NotePassage{}, err
	}
	if len(input.NoteID) != 32 {
		return NotePassage{}, toolErr("invalid_request", "The note ID is not valid.")
	}
	inScope, err := m.vault.AssistantNoteInScope(ctx, store.AssistantScope{
		Kind: run.Scope.Kind, NoteID: run.Scope.NoteID, NotebookID: run.Scope.NotebookID,
		IncludeDescendants: run.Scope.IncludeDescendants,
	}, input.NoteID)
	if err != nil {
		return NotePassage{}, toolErr("storage_failed", "The note could not be checked.")
	}
	note, err := m.vault.GetNote(input.NoteID)
	if err != nil {
		return NotePassage{}, toolErr("storage_failed", "The note could not be read.")
	}
	if note == nil || note.DeletedTime != 0 {
		return NotePassage{}, toolErr("note_missing", "That note no longer exists.")
	}
	if note.EncryptionApplied != 0 {
		return NotePassage{}, toolErr("encrypted_content", "That note is encrypted on this device.")
	}
	if note.IsConflict != 0 || !inScope {
		return NotePassage{}, toolErr("out_of_scope", "That note is outside the selected scope.")
	}
	version := noteVersion(note.ID, note.ParentID, note.Title, note.Body, note.UpdatedTime)
	if input.ExpectedVersion != "" && input.ExpectedVersion != version {
		return NotePassage{}, toolErr("note_changed", "The note changed since it was read. Read it again before citing it.")
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 6 << 10
	}
	if maxBytes > run.limits.ReadBytes {
		maxBytes = run.limits.ReadBytes
	}
	body := note.Body
	start := input.StartByte
	if start < 0 {
		start = 0
	}
	if start > len(body) {
		start = len(body)
	}
	for start < len(body) && !utf8.RuneStart(body[start]) {
		start++
	}
	end := start + maxBytes
	if end > len(body) {
		end = len(body)
	}
	for end > start && end < len(body) && !utf8.RuneStart(body[end]) {
		end--
	}
	if end < start {
		end = start
	}
	passage := body[start:end]
	if err := run.budget.addEvidence(len(passage)); err != nil {
		return NotePassage{}, err
	}
	notebook := ""
	if folder, err := m.vault.GetFolder(note.ParentID); err == nil && folder != nil {
		notebook = folder.Title
	}
	item := run.evidence.add(Evidence{
		NoteID:      note.ID,
		Title:       note.Title,
		Version:     version,
		StartByte:   start,
		EndByte:     end,
		Text:        passage,
		UpdatedTime: note.UpdatedTime,
	})
	lineStart := 1 + strings.Count(body[:start], "\n")
	lineEnd := lineStart + strings.Count(passage, "\n")
	result := NotePassage{
		NoteID:      note.ID,
		Title:       note.Title,
		Notebook:    notebook,
		Version:     version,
		UpdatedTime: note.UpdatedTime,
		StartByte:   start,
		EndByte:     end,
		LineStart:   lineStart,
		LineEnd:     lineEnd,
		Text:        passage,
		EvidenceID:  item.Token,
		Truncated:   end < len(body),
	}
	if end < len(body) {
		result.NextByte = end
	}
	m.publish(run, Event{
		Type: EventEvidenceRead,
		Citation: &Citation{
			Token: item.Token, NoteID: item.NoteID, Title: item.Title,
			Excerpt: excerpt(item.Text, 240), Version: item.Version, Verified: true,
		},
	})
	return result, nil
}

func (m *Manager) listNotebooks(ctx context.Context, run *Run) ([]map[string]string, error) {
	if err := run.budget.noteToolCall(); err != nil {
		return nil, err
	}
	folders, err := m.vault.AssistantNotebooks(ctx, store.AssistantScope{Kind: run.Scope.Kind})
	if err != nil {
		return nil, toolErr("storage_failed", "Notebooks could not be listed.")
	}
	out := make([]map[string]string, 0, len(folders))
	for _, folder := range folders {
		out = append(out, map[string]string{"id": folder.ID, "title": folder.Title, "parentId": folder.ParentID})
	}
	return out, nil
}

func (m *Manager) listTags(ctx context.Context, run *Run) ([]map[string]string, error) {
	if err := run.budget.noteToolCall(); err != nil {
		return nil, err
	}
	tags, err := m.vault.AssistantTags(ctx)
	if err != nil {
		return nil, toolErr("storage_failed", "Tags could not be listed.")
	}
	out := make([]map[string]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, map[string]string{"id": tag.ID, "title": tag.Title})
	}
	return out, nil
}
