package clipper

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// inlinePNG is a 1x1 PNG as the web clipper sends images whose URLs are gated:
// the bytes are the note's only copy, so they stay inline.
const inlinePNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestCreateNoteKeepsInlineImages(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	s := NewServer(db, testAPIToken, testAPIKey, nil, NewPolicy(&config.Config{MCPAllowFolders: "*"}), nil)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	nb := &models.Folder{Title: "Inbox"}
	if err := db.CreateFolder(nb); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	call := func(method, path, body string) *models.Note {
		t.Helper()
		req, err := http.NewRequest(method, ts.URL+path+"?token="+testAPIToken, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s = %d: %s", method, path, resp.StatusCode, raw)
		}
		var note models.Note
		if err := json.Unmarshal(raw, &note); err != nil {
			t.Fatalf("%s %s body = %q: %v", method, path, raw, err)
		}
		return &note
	}

	// A page clipped with its images inline in body_html keeps the exact bytes.
	html := `<p>Text</p><img src="` + inlinePNG + `" alt="inline">`
	fromHTML := call(http.MethodPost, "/notes",
		fmt.Sprintf(`{"title":"html","parent_id":%q,"body_html":%q}`, nb.ID, html))
	if !strings.Contains(fromHTML.Body, inlinePNG) {
		t.Fatalf("body_html image was not kept inline: %.160q", fromHTML.Body)
	}
	if !strings.Contains(fromHTML.Body, "![inline](") {
		t.Fatalf("body_html image did not become a Markdown image: %.160q", fromHTML.Body)
	}

	// image_data_url is the clipper's screenshot; it was previously dropped.
	shot := call(http.MethodPost, "/notes",
		fmt.Sprintf(`{"title":"shot","parent_id":%q,"image_data_url":%q}`, nb.ID, inlinePNG))
	if !strings.HasPrefix(shot.Body, "![Clipped image]("+inlinePNG+")") {
		t.Fatalf("image_data_url was not kept at the top of the body: %.160q", shot.Body)
	}

	// Updating with another screenshot keeps both copies rather than replacing.
	updated := call(http.MethodPut, "/notes/"+shot.ID,
		fmt.Sprintf(`{"image_data_url":%q}`, inlinePNG))
	if n := strings.Count(updated.Body, "![Clipped image]("+inlinePNG+")"); n != 2 {
		t.Fatalf("update kept %d inline images, want 2: %.160q", n, updated.Body)
	}

	resources, err := db.GetResourcesByNote(shot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 0 {
		t.Fatalf("inline images were converted to %d resources; they must stay inline", len(resources))
	}
}

func TestCreateNoteInlinesFetchedImages(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	s := NewServer(db, testAPIToken, testAPIKey, nil, NewPolicy(&config.Config{MCPAllowFolders: "*"}), nil)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	nb := &models.Folder{Title: "Inbox"}
	if err := db.CreateFolder(nb); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	photo, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(inlinePNG, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/photo.png" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(photo)
	}))
	defer imageSrv.Close()

	html := `<p>Text</p>` +
		`<img src="/photo.png" alt="Photo">` +
		`<img src="/photo.png" alt="Again">` +
		`<img src="/missing.png" alt="Missing">` +
		`<img src="` + inlinePNG + `" alt="Inline">`
	payload := fmt.Sprintf(`{"title":"clip","parent_id":%q,"body_html":%q,"base_url":%q}`,
		nb.ID, html, imageSrv.URL+"/article")
	note := postNoteForTest(t, ts, payload)

	if got := hits.Load(); got != 1 {
		t.Fatalf("fetched duplicate image %d times, want 1", got)
	}
	if strings.Contains(note.Body, imageSrv.URL+"/photo.png") {
		t.Fatalf("remote image URL survived: %.200q", note.Body)
	}
	if got := strings.Count(note.Body, "data:image/png;base64,"); got != 3 {
		t.Fatalf("body has %d inline images, want 3: %.200q", got, note.Body)
	}
	if !strings.Contains(note.Body, imageSrv.URL+"/missing.png") {
		t.Fatalf("failed image should stay as an absolute URL: %.200q", note.Body)
	}
}

func postNoteForTest(t *testing.T, ts *httptest.Server, body string) *models.Note {
	t.Helper()
	resp, err := http.Post(ts.URL+"/notes?token="+testAPIToken, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /notes = %d: %s", resp.StatusCode, raw)
	}
	var note models.Note
	if err := json.Unmarshal(raw, &note); err != nil {
		t.Fatalf("POST /notes body = %q: %v", raw, err)
	}
	return &note
}
