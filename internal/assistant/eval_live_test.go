package assistant

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

var liveEvalCases = []struct {
	Question  string
	WantTitle string
	WantNone  bool
}{
	{"What did I decide about the camera repair, and what is still outstanding?", "Camera repair", false},
	{"When is the tax filing deadline?", "Tax documents", false},
	{"When should I feed the sourdough starter and when should I bake?", "Sourdough schedule", false},
	{"Where are my flight and hotel details, and when should I check in?", "Travel bookings", false},
	{"What is my passport renewal status?", "", true},
}

// TestSplashEvaluation is the optional live companion to the deterministic
// retrieval evaluation. It measures the complete search -> read -> cite answer
// path and the refusal behavior on an absent note. It is not a CI gate because
// it requires a local model endpoint.
func TestSplashEvaluation(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("ASK_LIVE_BASE_URL"))
	if baseURL == "" {
		t.Skip("set ASK_LIVE_BASE_URL to run the live evaluation")
	}
	modelID := strings.TrimSpace(os.Getenv("ASK_LIVE_MODEL"))
	if modelID == "" {
		modelID = "main"
	}

	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	folder := &models.Folder{Title: "Evaluation corpus"}
	if err := db.CreateFolder(folder); err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{}
	for _, fixture := range evalCorpus {
		note := &models.Note{ParentID: folder.ID, Title: fixture.Title, Body: fixture.Body}
		if err := db.CreateNote(note); err != nil {
			t.Fatal(err)
		}
		notes[fixture.Title] = note.ID
	}

	events := make(chan Event, 512)
	m, err := NewManager(db, root, func(event Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = m.Close(ctx)
	}()
	settings := DefaultSettings()
	settings.BaseURL = baseURL
	settings.ModelID = modelID
	settings.APIKey = strings.TrimSpace(os.Getenv("ASK_LIVE_API_KEY"))
	settings.ContextWindow = 262144
	settings.MaxOutput = 4096
	settings.TimeoutMS = 300000
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}

	correct, validCitations, refusals := 0, 0, 0
	for _, test := range liveEvalCases {
		snapshot, err := m.NewConversation(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Start(AskRequest{
			ConversationID: snapshot.ConversationID,
			Text:           test.Question,
			Scope:          Scope{Kind: "vault"},
		}); err != nil {
			t.Fatal(err)
		}
		completed, err := waitForEvalRun(events, 3*time.Minute)
		if err != nil {
			t.Fatalf("%s: %v", test.Question, err)
		}
		citationCorrect := false
		allValid := true
		for _, citation := range completed.Citations {
			if !citation.Verified || citation.NoteID == "" {
				allValid = false
			}
			if test.WantTitle != "" && citation.NoteID == notes[test.WantTitle] {
				citationCorrect = true
			}
		}
		if len(completed.Citations) == 0 || allValid {
			validCitations++
		}
		refused := strings.Contains(strings.ToLower(completed.Text), "don't know") ||
			strings.Contains(strings.ToLower(completed.Text), "do not know") ||
			strings.Contains(strings.ToLower(completed.Text), "not in your notes") ||
			strings.Contains(strings.ToLower(completed.Text), "no information") ||
			strings.Contains(strings.ToLower(completed.Text), "couldn't find") ||
			strings.Contains(strings.ToLower(completed.Text), "could not find")
		if test.WantNone {
			if len(completed.Citations) == 0 && refused {
				refusals++
			}
		} else if citationCorrect {
			correct++
		}
		t.Logf("live eval question=%q want=%q citations=%d valid=%v refused=%v answer=%q",
			test.Question, test.WantTitle, len(completed.Citations), allValid, refused, completed.Text)
	}
	answerable := len(liveEvalCases) - 1
	t.Logf("live evaluation: correct_source=%d/%d citation_valid=%d/%d refusals=%d/1",
		correct, answerable, validCitations, len(liveEvalCases), refusals)
	if correct < answerable {
		t.Fatalf("live correct-source rate %d/%d; expected every answerable question to cite its note", correct, answerable)
	}
	if validCitations != len(liveEvalCases) {
		t.Fatalf("live citation validity %d/%d", validCitations, len(liveEvalCases))
	}
	if refusals != 1 {
		t.Fatalf("live refusal rate %d/1", refusals)
	}
}

func waitForEvalRun(events <-chan Event, timeout time.Duration) (Event, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			switch event.Type {
			case EventRunFailed:
				return Event{}, &evalRunError{event.Message}
			case EventRunCompleted:
				return event, nil
			}
		case <-timer.C:
			return Event{}, &evalRunError{"timed out waiting for the live evaluation run"}
		}
	}
}

type evalRunError struct{ message string }

func (e *evalRunError) Error() string { return e.message }
