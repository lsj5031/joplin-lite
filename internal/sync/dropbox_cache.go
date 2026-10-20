package sync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsj5031/joplin-lite/internal/store"
)

// cachedDropboxResource returns local bytes only when fresh Dropbox metadata
// verifies them. Ciphertext is returned unchanged for the normal decrypt path.
func cachedDropboxResource(db *store.DB, entry dropboxEntry) ([]byte, bool) {
	name := strings.TrimPrefix(entry.PathDisplay, "/")
	const prefix = ".resource/"
	if db == nil || entry.Tag != "file" || !strings.HasPrefix(name, prefix) {
		return nil, false
	}
	id := strings.TrimPrefix(name, prefix)
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return nil, false
	}
	want, err := hex.DecodeString(entry.ContentHash)
	if err != nil || len(want) != sha256.Size {
		return nil, false
	}
	// ResourceDir (also called by GetResourceFile) creates missing directories.
	// A cache miss must not create anything in the profile.
	dir := filepath.Join(db.DataDir(), "resources")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, false
	}
	for _, path := range []string{filepath.Join(dir, id+".crypted"), db.GetResourceFile(id)} {
		data, ok := verifiedDropboxCacheFile(path, want)
		if ok {
			return data, true
		}
	}
	return nil, false
}

func verifiedDropboxCacheFile(path string, want []byte) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	// Match the Dropbox download bound, without reading a truncated prefix or
	// letting ReadAll grow its allocation beyond that bound.
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return nil, false
	}
	data := make([]byte, int(info.Size()))
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, false
	}
	var extra [1]byte
	if n, err := f.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, false
	}
	// https://www.dropbox.com/developers/reference/content-hash
	overall := sha256.New()
	for remaining := data; len(remaining) > 0; {
		n := min(len(remaining), 4<<20)
		block := sha256.Sum256(remaining[:n])
		overall.Write(block[:])
		remaining = remaining[n:]
	}
	if !bytes.Equal(overall.Sum(nil), want) {
		return nil, false
	}
	return data, true
}
