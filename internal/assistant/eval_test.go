package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// evalCorpus is deliberately small and fixed: every change to retrieval or
// answer construction can be compared against the same questions.
var evalCorpus = []struct {
	Title string
	Body  string
}{
	{"Camera repair", "The camera repair was approved at the shop. Follow up about the lens."},
	{"Tax documents", "The accountant needs the tax documents before the March 31 filing deadline."},
	{"Sourdough schedule", "Feed the sourdough starter on Thursday evening and bake on Saturday morning."},
	{"Travel bookings", "Flight confirmation and hotel reservation are in the travel folder. Check in online on Friday."},
	{"Home internet", "If the router drops again, restart it and call the ISP support line."},
}

var evalQuestions = []struct {
	Query     string
	WantTitle string
	WantNone  bool
}{
	{"camera repair lens", "Camera repair", false},
	{"tax documents filing deadline", "Tax documents", false},
	{"sourdough starter bake", "Sourdough schedule", false},
	{"flight hotel confirmation", "Travel bookings", false},
	{"router ISP restart", "Home internet", false},
	{"passport renewal status", "", true},
}

// TestAskRetrievalEvaluation measures the actual BM25 retrieval path, not a
// model's paraphrase of it. It is deterministic and runs in normal CI.
func TestAskRetrievalEvaluation(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	folder := &models.Folder{Title: "Evaluations"}
	if err := db.CreateFolder(folder); err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]string, len(evalCorpus))
	for _, fixture := range evalCorpus {
		note := &models.Note{ParentID: folder.ID, Title: fixture.Title, Body: fixture.Body}
		if err := db.CreateNote(note); err != nil {
			t.Fatal(err)
		}
		ids[fixture.Title] = note.ID
	}

	hits, topOne, correctSource, refusals := 0, 0, 0, 0
	for _, question := range evalQuestions {
		results, err := db.AssistantSearch(context.Background(), store.AssistantSearchQuery{
			Query: question.Query,
			Scope: store.AssistantScope{Kind: "vault"},
			Limit: 5,
		})
		if err != nil {
			t.Fatalf("%s: %v", question.Query, err)
		}
		if question.WantNone {
			if len(results) == 0 {
				refusals++
			}
			t.Logf("retrieval query=%q expected=no-hit got=%d", question.Query, len(results))
			continue
		}
		found := false
		for _, result := range results {
			if result.NoteID == ids[question.WantTitle] {
				found = true
				break
			}
		}
		if found {
			hits++
			correctSource++
			if len(results) > 0 && results[0].NoteID == ids[question.WantTitle] {
				topOne++
			}
		}
		t.Logf("retrieval query=%q expected=%q found=%v", question.Query, question.WantTitle, found)
	}

	answerable := len(evalQuestions) - 1
	if hits*100/answerable < 80 {
		t.Fatalf("retrieval hit rate = %d/%d, want at least 80%%", hits, answerable)
	}
	if topOne*100/answerable < 60 {
		t.Fatalf("correct top-1 source rate = %d/%d, want at least 60%%", topOne, answerable)
	}
	if refusals != 1 {
		t.Fatalf("unanswerable query was not left empty: refusals=%d", refusals)
	}
	if correctSource != hits {
		t.Fatalf("source metrics diverged: correctSource=%d hits=%d", correctSource, hits)
	}

	// Citation validity is a separate invariant from retrieval: only a passage
	// read through evidence.add can become a clickable citation.
	registry := newEvidenceRegistry()
	note := &models.Note{ParentID: folder.ID, Title: "Camera repair", Body: evalCorpus[0].Body}
	note.ID = ids["Camera repair"]
	version := noteVersion(note.ID, note.ParentID, note.Title, note.Body, note.UpdatedTime)
	registry.add(Evidence{NoteID: note.ID, Title: note.Title, Version: version, StartByte: 0, EndByte: len(note.Body), Text: note.Body})
	citations := registry.cite("The repair was approved [E1], and an invented token [E99] is not a source.")
	if len(citations) != 1 || !citations[0].Verified || citations[0].NoteID != note.ID {
		t.Fatalf("citation validation failed: %+v", citations)
	}
	if strings.Contains(citations[0].Token, "99") {
		t.Fatalf("unknown evidence token was accepted: %+v", citations)
	}

	t.Logf("ask evaluation: hit=%d/%d top1=%d/%d citation_valid=1/1 refusals=%d/%d",
		hits, answerable, topOne, answerable, refusals, 1)
}
