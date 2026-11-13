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

// TestSplashLiveCitedAnswer exercises the real embedded agent against a locally
// running Splash service. It is opt-in so the normal suite never depends on a
// model or a user endpoint.
func TestSplashLiveCitedAnswer(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("ASK_LIVE_BASE_URL"))
	if baseURL == "" {
		t.Skip("set ASK_LIVE_BASE_URL to run the live Splash test")
	}
	modelID := strings.TrimSpace(os.Getenv("ASK_LIVE_MODEL"))
	if modelID == "" {
		modelID = "main"
	}
	apiKey := strings.TrimSpace(os.Getenv("ASK_LIVE_API_KEY"))

	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	folder := &models.Folder{Title: "Camera"}
	if err := db.CreateFolder(folder); err != nil {
		t.Fatal(err)
	}
	note := &models.Note{
		ParentID: folder.ID,
		Title:    "Camera repair",
		Body:     "The camera repair was approved at the shop. Follow up about the lens.",
	}
	if err := db.CreateNote(note); err != nil {
		t.Fatal(err)
	}

	events := make(chan Event, 256)
	m, err := NewManager(db, root, func(event Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = m.Close(ctx)
	}()

	settings := DefaultSettings()
	settings.BaseURL = baseURL
	settings.ModelID = modelID
	settings.APIKey = apiKey
	settings.ContextWindow = 262144
	settings.MaxOutput = 4096
	settings.TimeoutMS = 300000
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Start(AskRequest{
		Text:  "What did I decide about the camera repair, and what follow-up is outstanding? Cite the note.",
		Scope: Scope{Kind: "note", NoteID: note.ID},
	}); err != nil {
		t.Fatal(err)
	}

	timeout := time.After(5 * time.Minute)
	for {
		select {
		case event := <-events:
			switch event.Type {
			case EventRunStarted, EventActivity, EventEvidenceRead, EventCitation:
				t.Logf("event=%s activity=%+v citation=%+v", event.Type, event.Activity, event.Citation)
			case EventRunFailed:
				t.Fatalf("live run failed: %s %s", event.Code, event.Message)
			case EventRunCompleted:
				t.Logf("completed text=%q citations=%+v", event.Text, event.Citations)
				if !strings.Contains(event.Text, "[E1]") {
					t.Fatalf("live answer lost its citation: %q", event.Text)
				}
				if len(event.Citations) != 1 || event.Citations[0].NoteID != note.ID || !event.Citations[0].Verified {
					t.Fatalf("live citation was not verified: %+v", event.Citations)
				}
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for Splash")
		}
	}
}
