package dropboxrepair

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

const testID = "11111111111111111111111111111111"
const testKey = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func envelope() []byte {
	packet := `{"salt":"AAAAAAAAAAA=","iv":"AAAAAAAAAAA=","ct":"AAAAAAAAAAA="}`
	ciphertext := "JED010000220a" + testKey + fmt.Sprintf("%06x", len(packet)) + packet
	return []byte(fmt.Sprintf("id: %s\nupdated_time: 2026-02-25T00:00:00.000Z\nencryption_cipher_text: %s\nencryption_applied: 1\ntype_: 1", testID, ciphertext))
}

type fakeDropbox struct {
	mu                 sync.Mutex
	files              map[string][]byte
	revs               map[string]string
	uploads, downloads int
	fail               string
	backup             string
	t                  *testing.T
}

func (f *fakeDropbox) meta(path string) Metadata {
	return Metadata{Tag: "file", Path: path, Rev: f.revs[path], Hash: ContentHash(f.files[path]), Size: int64(len(f.files[path]))}
}

func fixture(t *testing.T) (*fakeDropbox, *Client, string) {
	t.Helper()
	f := &fakeDropbox{t: t, files: map[string][]byte{
		"/info.json":         []byte(`{"version":3,"masterKeys":[{"id":"` + testKey + `"}]}`),
		"/" + testID + ".md": envelope(),
		"/.resource/blob":    []byte("untouched blob"),
	}, revs: map[string]string{"/info.json": "r1", "/" + testID + ".md": "r1", "/.resource/blob": "r1"}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	c := NewClient("synthetic-token")
	c.API, c.Content = server.URL, server.URL
	backup := filepath.Join(t.TempDir(), "backup")
	f.backup = backup
	return f, c, backup
}

func (f *fakeDropbox) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer synthetic-token" {
		f.t.Error("authorization missing")
	}
	switch r.URL.Path {
	case "/2/files/list_folder":
		var entries []Metadata
		for p := range f.files {
			entries = append(entries, f.meta(p))
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		if f.fail == "inventory-drift" && f.downloads > 1 {
			f.revs["/.resource/blob"] = "changed-during-scan"
			for i := range entries {
				if entries[i].Path == "/.resource/blob" {
					entries[i].Rev = "changed-during-scan"
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"entries": entries, "cursor": "opaque", "has_more": false})
	case "/2/files/download":
		var arg struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(r.Header.Get("Dropbox-API-Arg")), &arg)
		if arg.Path == "/.resource/blob" {
			f.t.Error("attachment downloaded")
		}
		f.downloads++
		if f.fail == "rate-read" {
			f.fail = ""
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		raw, ok := f.files[arg.Path]
		if !ok {
			w.WriteHeader(409)
			return
		}
		downloadMeta := f.meta(arg.Path)
		downloadMeta.Tag = "" // Real /download returns FileMetadata, not a union.
		meta, _ := json.Marshal(downloadMeta)
		w.Header().Set("Dropbox-API-Result", string(meta))
		w.Write(raw)
	case "/2/files/upload":
		var arg struct {
			Path string `json:"path"`
			Mode struct {
				Tag string `json:".tag"`
				Rev string `json:"update"`
			} `json:"mode"`
			Autorename bool `json:"autorename"`
			Strict     bool `json:"strict_conflict"`
		}
		if json.Unmarshal([]byte(r.Header.Get("Dropbox-API-Arg")), &arg) != nil || !itemPath.MatchString(arg.Path) || arg.Mode.Tag != "update" || arg.Autorename || !arg.Strict {
			f.t.Error("unsafe upload arguments")
			w.WriteHeader(400)
			return
		}
		m, err := Load(f.backup)
		if err != nil || len(m.Entries) == 0 {
			f.t.Error("upload preceded durable full backup")
		}
		f.uploads++
		if f.fail == "rate-write" {
			f.fail = ""
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		if f.fail == "long-rate-write" {
			w.Header().Set("Retry-After", "180")
			w.WriteHeader(429)
			return
		}
		if f.fail == "conflict" {
			f.fail = ""
			f.revs[arg.Path] = "concurrent"
			w.WriteHeader(409)
			return
		}
		if arg.Mode.Rev != f.revs[arg.Path] {
			w.WriteHeader(409)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		f.files[arg.Path] = raw
		f.revs[arg.Path] = fmt.Sprintf("r%d", f.uploads+1)
		if f.fail == "uncertain" {
			f.fail = ""
			w.WriteHeader(500) // write committed, response lost
			return
		}
		json.NewEncoder(w).Encode(f.meta(arg.Path))
	default:
		f.t.Errorf("unexpected endpoint %s", r.URL.Path)
		w.WriteHeader(400)
	}
}

func TestUnstableInventoryCannotApply(t *testing.T) {
	f, c, backup := fixture(t)
	f.fail = "inventory-drift"
	if _, err := Prepare(context.Background(), c, backup, []string{testKey}); err == nil {
		t.Fatal("unstable target accepted")
	}
	if _, err := os.Stat(filepath.Join(backup, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("failed preparation produced manifest")
	}
	if _, err := Apply(context.Background(), c, backup, false); err == nil || f.uploads != 0 {
		t.Fatal("failed preparation applied")
	}
}

func TestCrashResumeAndWholeBackupPreflight(t *testing.T) {
	f, c, backup := fixture(t)
	secondID := "22222222222222222222222222222222"
	f.files["/"+secondID+".md"] = bytes.ReplaceAll(envelope(), []byte(testID), []byte(secondID))
	f.revs["/"+secondID+".md"] = "r1"
	m, err := Prepare(context.Background(), c, backup, []string{testKey})
	if err != nil || len(m.Entries) != 2 {
		t.Fatalf("prepare: %v", err)
	}
	// A bad final backup must prevent even the first candidate's write.
	path := filepath.Join(backup, secondID+".md")
	second, _ := os.ReadFile(path)
	os.WriteFile(path, []byte("corrupt"), 0600)
	if _, err := Apply(context.Background(), c, backup, false); err == nil || f.uploads != 0 {
		t.Fatal("incomplete preflight wrote")
	}
	os.WriteFile(path, second, 0600)
	// Simulate crash after a successful upload but before a success journal.
	_, after, err := original(backup, m.Entries[0])
	if err != nil {
		t.Fatal(err)
	}
	f.files["/"+testID+".md"] = after
	f.revs["/"+testID+".md"] = "committed-before-crash"
	if n, err := Apply(context.Background(), c, backup, false); err != nil || n != 2 || f.uploads != 1 {
		t.Fatalf("crash resume: %v", err)
	}
	for _, e := range m.Entries {
		stat, err := os.Stat(filepath.Join(backup, e.Path[1:]))
		if err != nil || stat.Mode().Perm() != 0600 {
			t.Fatal("unprotected original backup")
		}
	}
}

func TestLongRetryAfterAndOperatorLock(t *testing.T) {
	f, c, backup := fixture(t)
	if _, err := Prepare(context.Background(), c, backup, []string{testKey}); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockBackup(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), c, backup, false); err == nil || f.uploads != 0 {
		t.Fatal("concurrent operator accepted")
	}
	unlock()
	f.fail = "long-rate-write"
	if _, err := Apply(context.Background(), c, backup, false); err == nil || f.uploads != 1 {
		t.Fatal("long Retry-After retried early")
	}
}

func TestVerificationReportsConcurrentUnrelatedChanges(t *testing.T) {
	f, c, backup := fixture(t)
	if _, err := Prepare(context.Background(), c, backup, []string{testKey}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), c, backup, false); err != nil {
		t.Fatal(err)
	}
	f.files["/.resource/blob"] = []byte("concurrent legitimate edit")
	f.revs["/.resource/blob"] = "concurrent"
	scan := filepath.Join(t.TempDir(), "verify")
	v, err := Verify(context.Background(), c, backup, scan, []string{testKey})
	if err != nil || v.Verified != 1 || v.Affected != 0 || len(v.Unrelated) != 1 || v.Unrelated[0].Path != "/.resource/blob" {
		t.Fatalf("verification: %#v %v", v, err)
	}
	if string(f.files["/.resource/blob"]) != "concurrent legitimate edit" {
		t.Fatal("concurrent edit overwritten")
	}
	// A newer revision even with identical repaired bytes must not be hidden.
	f.revs["/"+testID+".md"] = "later"
	if _, err := Verify(context.Background(), c, backup, filepath.Join(t.TempDir(), "verify"), []string{testKey}); err == nil {
		t.Fatal("newer repair revision accepted")
	}
}

func TestRemoteLifecycle(t *testing.T) {
	f, c, backup := fixture(t)
	original := bytes.Clone(f.files["/"+testID+".md"])
	m, err := Prepare(context.Background(), c, backup, []string{testKey})
	if err != nil || len(m.Entries) != 1 || f.uploads != 0 {
		t.Fatalf("prepare: %v", err)
	}
	n, err := Apply(context.Background(), c, backup, false)
	if err != nil || n != 1 || f.uploads != 1 {
		t.Fatalf("apply: %d %v", n, err)
	}
	if !bytes.HasSuffix(f.files["/"+testID+".md"], original) {
		t.Fatal("existing bytes changed")
	}
	if n, err = Apply(context.Background(), c, backup, false); err != nil || n != 1 || f.uploads != 1 {
		t.Fatalf("resume: %v", err)
	}
	m, err = Prepare(context.Background(), c, filepath.Join(t.TempDir(), "rescan"), []string{testKey})
	if err != nil || len(m.Entries) != 0 {
		t.Fatalf("rescan: %v", err)
	}
	if n, err = Apply(context.Background(), c, backup, true); err != nil || n != 1 {
		t.Fatalf("rollback: %v", err)
	}
	if !bytes.Equal(f.files["/"+testID+".md"], original) {
		t.Fatal("rollback did not restore exact original")
	}
	if _, err = Apply(context.Background(), c, backup, true); err != nil || f.uploads != 2 {
		t.Fatalf("rollback resume: %v", err)
	}
}

func TestUploadFailureReconciliation(t *testing.T) {
	for _, mode := range []string{"uncertain", "rate-write", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			f, c, backup := fixture(t)
			if _, err := Prepare(context.Background(), c, backup, []string{testKey}); err != nil {
				t.Fatal(err)
			}
			f.fail = mode
			n, err := Apply(context.Background(), c, backup, false)
			if mode == "conflict" {
				if err == nil || f.uploads != 1 || n != 0 {
					t.Fatal("conflict overwritten")
				}
			} else if err != nil || n != 1 {
				t.Fatalf("reconcile: %v", err)
			}
			if mode == "uncertain" && f.uploads != 1 {
				t.Fatal("uncertain committed write retried")
			}
		})
	}
}

func TestPreflightAndRollbackGuards(t *testing.T) {
	for _, mode := range []string{"wrong-target", "bad-envelope", "tampered-backup", "newer-original", "rollback-newer", "missing-journal", "rate-read"} {
		t.Run(mode, func(t *testing.T) {
			f, c, backup := fixture(t)
			keys := []string{testKey}
			switch mode {
			case "wrong-target":
				keys = []string{testID}
			case "bad-envelope":
				f.files["/"+testID+".md"] = []byte("bad metadata")
			case "rate-read":
				f.fail = mode
			}
			_, err := Prepare(context.Background(), c, backup, keys)
			if mode == "wrong-target" || mode == "bad-envelope" {
				if err == nil || f.uploads != 0 {
					t.Fatal("unsafe preflight")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "rate-read" {
				return
			}
			rollback := false
			switch mode {
			case "tampered-backup":
				os.WriteFile(filepath.Join(backup, testID+".md"), []byte("bad"), 0600)
			case "newer-original":
				f.revs["/"+testID+".md"] = "newer"
			case "rollback-newer":
				if _, err := Apply(context.Background(), c, backup, false); err != nil {
					t.Fatal(err)
				}
				f.revs["/"+testID+".md"] = "newer"
				rollback = true
			case "missing-journal":
				if _, err := Apply(context.Background(), c, backup, false); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(backup, "success", testID+".md.json")); err != nil {
					t.Fatal(err)
				}
				rollback = true
			}
			before := f.uploads
			if _, err := Apply(context.Background(), c, backup, rollback); err == nil || f.uploads != before {
				t.Fatal("guard failed")
			}
		})
	}
}

func TestRollbackPartialApply(t *testing.T) {
	f, c, backup := fixture(t)
	secondID := "22222222222222222222222222222222"
	second := bytes.ReplaceAll(envelope(), []byte(testID), []byte(secondID))
	f.files["/"+secondID+".md"] = second
	f.revs["/"+secondID+".md"] = "r1"
	m, err := Prepare(context.Background(), c, backup, []string{testKey})
	if err != nil {
		t.Fatal(err)
	}
	// A partial apply committed/journaled only the first entry.
	e := m.Entries[0]
	_, fixed, err := original(backup, e)
	if err != nil {
		t.Fatal(err)
	}
	f.files[e.Path], f.revs[e.Path] = fixed, "committed"
	dir := filepath.Join(backup, "success")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := record(filepath.Join(dir, e.Path[1:]+".json"), Success{Path: e.Path, Rev: "committed", Hash: ContentHash(fixed), SHA256: sha(fixed)}); err != nil {
		t.Fatal(err)
	}
	n, err := Apply(context.Background(), c, backup, true)
	if err != nil || n != 1 || f.uploads != 1 || !bytes.Equal(f.files["/"+secondID+".md"], second) {
		t.Fatalf("partial rollback: %v", err)
	}
}
