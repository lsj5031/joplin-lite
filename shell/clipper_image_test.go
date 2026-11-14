package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/clipper"
	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// TestClipperInlineImageRenders follows the reported path: the official web
// clipper creates a note through the Data API with a base64 image (either
// body_html or image_data_url), and the native preview paints it. URLs are not
// fetched: gated images survive only as the bytes the clipper sent.
func TestClipperInlineImageRenders(t *testing.T) {
	registerFonts()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	nb := &models.Folder{Title: "Inbox"}
	if err := db.CreateFolder(nb); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	srv := clipper.NewServer(db, "token", "key", nil,
		clipper.NewPolicy(&config.Config{MCPAllowFolders: "*"}), nil)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	picture := image.NewRGBA(image.Rect(0, 0, 64, 64))
	red := color.RGBA{220, 40, 60, 255}
	draw.Draw(picture, picture.Bounds(), &image.Uniform{red}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(encoded.Bytes())
	}))
	defer imageSrv.Close()

	for _, tc := range []struct{ name, label, payload string }{
		{"image_data_url", "Clipped image", fmt.Sprintf(`{"title":"Shot","parent_id":%q,"image_data_url":%q}`, nb.ID, dataURL)},
		{"body_html", "Clipped image", fmt.Sprintf(`{"title":"HTML shot","parent_id":%q,"body_html":%q}`,
			nb.ID, `<p>Text</p><img src="`+dataURL+`" alt="Clipped image">`)},
		{"fetched_url", "Fetched image", fmt.Sprintf(`{"title":"Fetched shot","parent_id":%q,"body_html":%q,"base_url":%q}`,
			nb.ID, `<p>Text</p><img src="/photo.png" alt="Fetched image">`, imageSrv.URL+"/article")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(ts.URL+"/notes?token=token", "application/json", strings.NewReader(tc.payload))
			if err != nil {
				t.Fatalf("clip note: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("clip note = %d", resp.StatusCode)
			}
			var note models.Note
			if err := json.NewDecoder(resp.Body).Decode(&note); err != nil {
				t.Fatalf("decode note: %v", err)
			}
			if !strings.Contains(note.Body, dataURL) {
				t.Fatalf("clipped body lost the inline image: %.160q", note.Body)
			}

			app := newNativeApp(db)
			app.reloadNotes()
			app.showNote(note.ID)
			tt := ui.NewTester(app.view, 1280, 800)
			deadline := time.Now().Add(5 * time.Second)
			for {
				tt.Frame()
				r, ok := tt.Find(tc.label)
				if ok && r.W >= 60 && r.H >= 60 {
					frame := tt.Image()
					got := color.RGBAModel.Convert(frame.At(int(r.X+r.W/2), int(r.Y+r.H/2)))
					if got == red {
						break
					}
				}
				if time.Now().After(deadline) {
					t.Fatalf("clipped image did not paint: %.160q", app.body)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
				capture(t, dir, "clipper-"+tc.name, tt)
			}
		})
	}
}
