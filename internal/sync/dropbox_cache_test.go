package sync

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

func TestCachedDropboxResourceContentHash(t *testing.T) {
	// Expected hashes generated with Dropbox's reference implementation:
	// https://github.com/dropbox/dropbox-api-content-hasher/blob/master/python/dropbox_content_hasher.py
	// The input is 'a' repeated size times; these cover no block, a partial
	// block, and both sides of the 4 MiB boundary (including exact multiples).
	for _, tc := range []struct {
		size int
		hash string
	}{
		{0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{1, "bf5d3affb73efd2ec6c36ad3112dd933efed63c4e1cbffcfa88e2759c144f2d8"},
		{4194303, "49c0a79929eacf8ccbe77f99ff043ca55f97000c68bccefc644791170969a3b2"},
		{4194304, "907a506cf5e706bda5c7a29b43c9c65d8344bd2fa2f22339b359c214812af5a1"},
		{4194305, "5f858b62ccd88447586305aec6fd53c96747cfebf527cbba129a6dfed47d9624"},
		{8388608, "941162ca0d3fcd4e4b5bcaf179e3a156ae2b66b39e87993a7e626e61938ab2c9"},
	} {
		t.Run(fmt.Sprint(tc.size), func(t *testing.T) {
			db := testDB(t)
			id := "0123456789abcdef0123456789abcdef"
			data := bytes.Repeat([]byte("a"), tc.size)
			if err := os.WriteFile(filepath.Join(db.ResourceDir(), id+".crypted"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			entry := dropboxEntry{Tag: "file", PathDisplay: "/.resource/" + id, ContentHash: tc.hash}
			if got, ok := cachedDropboxItem(db, entry); !ok || !bytes.Equal(got, data) {
				t.Fatalf("cache hit = %v, bytes = %d; want exact %d bytes", ok, len(got), len(data))
			}
			if tc.size != 0 {
				entry.ContentHash = fmt.Sprintf("%x", sha256.Sum256(data))
				if _, ok := cachedDropboxItem(db, entry); ok {
					t.Fatal("accepted ordinary SHA-256 instead of Dropbox content hash")
				}
			}
		})
	}
}

func TestCachedDropboxResourceCandidates(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	const hash = "bf5d3affb73efd2ec6c36ad3112dd933efed63c4e1cbffcfa88e2759c144f2d8"
	for _, tc := range []struct {
		name, crypted, normal, extension string
		want                             bool
	}{
		{"crypted", "a", "decrypted", "png", true},
		{"extensionless", "stale", "a", "", true},
		{"official-extension", "stale", "a", "png", true},
		{"missing-crypted", "", "a", "png", true},
		{"mismatched", "stale", "decrypted", "png", false},
		{"missing", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			if err := db.UpsertResource(&models.Resource{ID: id, FileExtension: tc.extension}); err != nil {
				t.Fatal(err)
			}
			dir := db.ResourceDir()
			files := map[string]string{filepath.Join(dir, id+".crypted"): tc.crypted}
			normal := filepath.Join(dir, id)
			if tc.extension != "" {
				normal += "." + tc.extension
			}
			files[normal] = tc.normal
			for path, data := range files {
				if data != "" {
					if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			var before, after int
			if err := db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
				t.Fatal(err)
			}
			got, ok := cachedDropboxItem(db, dropboxEntry{Tag: "file", PathDisplay: ".resource/" + id, ContentHash: hash})
			if ok != tc.want || (ok && string(got) != "a") || (!ok && got != nil) {
				t.Fatalf("got %q, %v; want hit %v", got, ok, tc.want)
			}
			if err := db.QueryRow("SELECT total_changes()").Scan(&after); err != nil || before != after {
				t.Fatalf("database mutated: before %d, after %d, err %v", before, after, err)
			}
			for path, data := range files {
				got, err := os.ReadFile(path)
				if data == "" {
					if !os.IsNotExist(err) {
						t.Fatalf("missing cache changed: %s: %v", path, err)
					}
				} else if err != nil || string(got) != data {
					t.Fatalf("cache changed: %s: %q, %v", path, got, err)
				}
			}
		})
	}
}

func TestCachedDropboxResourceRejectsInvalidOrUnavailable(t *testing.T) {
	db := testDB(t)
	const id = "0123456789abcdef0123456789abcdef"
	const hash = "bf5d3affb73efd2ec6c36ad3112dd933efed63c4e1cbffcfa88e2759c144f2d8"
	dir := filepath.Join(db.DataDir(), "resources")
	valid := dropboxEntry{Tag: "file", PathDisplay: "/.resource/" + id, ContentHash: hash}
	if _, ok := cachedDropboxItem(db, valid); ok {
		t.Fatal("accepted missing directory")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cache lookup created resources directory: %v", err)
	}
	if _, ok := cachedDropboxItem(nil, valid); ok {
		t.Fatal("accepted nil database")
	}
	if err := os.WriteFile(filepath.Join(db.ResourceDir(), id+".crypted"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/" + id + ".md", "/other/" + id, "/.resource/" + strings.ToUpper(id),
		"/.resource/" + id[:31], "/.resource/" + id + "0", "/.resource/../" + id,
		"//.resource/" + id, "/.resource/" + id + ".crypted", "/.resource/" + strings.Repeat("g", 32),
	} {
		entry := valid
		entry.PathDisplay = path
		if _, ok := cachedDropboxItem(db, entry); ok {
			t.Errorf("accepted invalid path %q", path)
		}
	}
	for _, hash := range []string{"", "invalid", strings.Repeat("0", 62), strings.Repeat("0", 66), strings.Repeat("0", 64)} {
		entry := valid
		entry.ContentHash = hash
		if _, ok := cachedDropboxItem(db, entry); ok {
			t.Errorf("accepted hash %q", hash)
		}
	}
	entry := valid
	entry.Tag = "folder"
	if _, ok := cachedDropboxItem(db, entry); ok {
		t.Fatal("accepted folder")
	}
	// Directories cannot be read as cache files. An oversized sparse file must
	// fall back without allocating/reading its contents.
	crypted := filepath.Join(dir, id+".crypted")
	if err := os.Remove(crypted); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(crypted, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok := cachedDropboxItem(db, valid); ok {
		t.Fatal("accepted directory cache")
	}
	if err := os.Remove(crypted); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(crypted)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate((256 << 20) + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := cachedDropboxItem(db, valid); ok {
		t.Fatal("accepted oversized cache")
	}
}
