package dropboxrepair

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lsj5031/joplin-lite/internal/enveloperepair"
)

var itemPath = regexp.MustCompile(`^/[a-f0-9]{32}\.md$`)

type Entry struct {
	Metadata
	Before string `json:"before_sha256"`
	After  string `json:"after_sha256"`
}

type Manifest struct {
	Version    int        `json:"version"`
	Created    string     `json:"created"`
	Identity   string     `json:"identity"`
	MasterKeys []string   `json:"master_key_ids"`
	InfoSHA256 string     `json:"info_sha256"`
	Cursor     string     `json:"cursor"`
	Inventory  []Metadata `json:"inventory"`
	Entries    []Entry    `json:"entries"`
}

type Success struct {
	Path   string `json:"path"`
	Rev    string `json:"rev"`
	Hash   string `json:"content_hash"`
	SHA256 string `json:"sha256"`
}

func sha(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func save(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func saveJSON(path string, data any) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return save(path, append(raw, '\n'))
}

// Link a fully flushed temporary journal into place without replacing an
// existing success. A crash cannot leave a truncated final journal.
func record(path string, data Success) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".journal-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(f.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func normalize(in []Metadata) ([]Metadata, error) {
	out := slices.Clone(in)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	for i, m := range out {
		if !strings.HasPrefix(m.Path, "/") || (i > 0 && m.Path == out[i-1].Path) {
			return nil, errors.New("invalid or duplicate inventory path")
		}
		if m.Tag == "file" && (m.Rev == "" || len(m.Hash) != 64) {
			return nil, errors.New("incomplete file inventory")
		}
	}
	return out, nil
}

// Prepare is read-only remotely. It writes private backups of affected items
// and a stable complete metadata inventory, never attachment payloads.
func Prepare(ctx context.Context, c *Client, backup string, expectedKeys []string) (*Manifest, error) {
	if !filepath.IsAbs(backup) || len(expectedKeys) == 0 {
		return nil, errors.New("absolute new backup and expected master keys required")
	}
	if err := os.Mkdir(backup, 0700); err != nil {
		return nil, err
	}
	if err := syncDir(filepath.Dir(backup)); err != nil {
		return nil, err
	}
	c.progress("checking target identity", 0)
	info, infoMeta, err := c.Download(ctx, "/info.json")
	if err != nil {
		return nil, fmt.Errorf("read target identity: %w", err)
	}
	var target struct {
		Version int `json:"version"`
		Keys    []struct {
			ID string `json:"id"`
		} `json:"masterKeys"`
	}
	if json.Unmarshal(info, &target) != nil || target.Version != 3 {
		return nil, errors.New("not an expected Joplin v3 target")
	}
	var ids []string
	for _, key := range target.Keys {
		ids = append(ids, key.ID)
	}
	sort.Strings(ids)
	expectedKeys = slices.Clone(expectedKeys)
	sort.Strings(expectedKeys)
	if !slices.Equal(ids, expectedKeys) {
		return nil, errors.New("remote master key IDs do not match selected official profile")
	}
	c.progress("master key IDs matched", len(ids))
	inv, cursor, err := c.Inventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("list target inventory: %w", err)
	}
	inv, err = normalize(inv)
	if err != nil {
		return nil, err
	}
	foundInfo := false
	for _, meta := range inv {
		if meta.Path == "/info.json" {
			foundInfo = meta == infoMeta
		}
	}
	if !foundInfo {
		return nil, errors.New("target identity changed during inventory")
	}
	c.progress("inventory paths", len(inv))
	m := &Manifest{Version: 1, Created: time.Now().UTC().Format(time.RFC3339), Identity: "official-joplin-app-folder:m044w3cvmxhzvop", MasterKeys: ids, InfoSHA256: sha(info), Cursor: cursor, Inventory: inv}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var firstErr error
	var readCount int
	jobs := make(chan Metadata)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for meta := range jobs {
				raw, fresh, e := c.Download(ctx, meta.Path)
				if e == nil && fresh != meta {
					e = errors.New("inventory changed during download; prepare again")
				}
				var fixed []byte
				if e == nil {
					fixed, e = enveloperepair.Correct(raw, strings.TrimSuffix(strings.TrimPrefix(meta.Path, "/"), ".md"))
				}
				if e == nil && !bytes.Equal(raw, fixed) {
					e = save(filepath.Join(backup, meta.Path[1:]), raw)
					if e == nil {
						mu.Lock()
						m.Entries = append(m.Entries, Entry{Metadata: meta, Before: sha(raw), After: sha(fixed)})
						mu.Unlock()
					}
				}
				if e != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", meta.Path, e)
						cancel()
					}
					mu.Unlock()
				}
				mu.Lock()
				readCount++
				if readCount%250 == 0 {
					c.progress("item metadata read", readCount)
				}
				mu.Unlock()
			}
		}()
	}
send:
	for _, meta := range inv {
		if meta.Tag == "file" && itemPath.MatchString(meta.Path) {
			select {
			case jobs <- meta:
			case <-ctx.Done():
				break send
			}
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	c.progress("item metadata read", readCount)
	again, _, err := c.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	again, err = normalize(again)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(inv, again) {
		return nil, errors.New("target changed during preparation; no writes made, prepare again")
	}
	c.progress("stable inventory; affected backups flushed", len(m.Entries))
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	if err := saveJSON(filepath.Join(backup, "manifest.json"), m); err != nil {
		return nil, err
	}
	if err := syncDir(backup); err != nil {
		return nil, err
	}
	return m, nil
}

func Load(backup string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(backup, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if json.Unmarshal(raw, &m) != nil || m.Version != 1 || m.Identity != "official-joplin-app-folder:m044w3cvmxhzvop" {
		return nil, errors.New("invalid repair manifest")
	}
	inventory, err := normalize(m.Inventory)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]Metadata, len(inventory))
	for _, meta := range inventory {
		byPath[meta.Path] = meta
	}
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if !itemPath.MatchString(e.Path) || seen[e.Path] || e.Rev == "" || e.Metadata != byPath[e.Path] {
			return nil, errors.New("invalid manifest entry")
		}
		seen[e.Path] = true
		if _, _, err := original(backup, e); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

func original(backup string, e Entry) ([]byte, []byte, error) {
	path := filepath.Join(backup, e.Path[1:])
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() {
		return nil, nil, errors.New("backup is not a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	fixed, err := enveloperepair.Correct(raw, strings.TrimSuffix(e.Path[1:], ".md"))
	if err != nil || sha(raw) != e.Before || sha(fixed) != e.After || ContentHash(raw) != e.Hash || bytes.Equal(raw, fixed) {
		return nil, nil, errors.New("backup/manifest integrity check failed")
	}
	return raw, fixed, nil
}

// Apply is resumable: every attempt starts with an exact download. An uncertain
// upload is reconciled by bytes and revision, never retried blindly.
// rollback uses the durable success revision, not the original revision.
func Apply(ctx context.Context, c *Client, backup string, rollback bool) (int, error) {
	m, err := Load(backup)
	if err != nil {
		return 0, err
	}
	unlock, err := lockBackup(backup)
	if err != nil {
		return 0, err
	}
	defer unlock()
	info, _, err := c.Download(ctx, "/info.json")
	if err != nil {
		return 0, err
	}
	if sha(info) != m.InfoSHA256 {
		return 0, errors.New("target info changed; reconcile before applying")
	}
	journalDir := filepath.Join(backup, "success")
	if rollback {
		journalDir = filepath.Join(backup, "rollback")
	}
	if err := os.MkdirAll(journalDir, 0700); err != nil {
		return 0, err
	}
	if err := syncDir(backup); err != nil {
		return 0, err
	}
	count := 0
	for _, e := range m.Entries {
		before, after, err := original(backup, e)
		if err != nil {
			return count, err
		}
		rev := e.Rev
		if rollback {
			raw, err := os.ReadFile(filepath.Join(backup, "success", e.Path[1:]+".json"))
			if os.IsNotExist(err) {
				// An interrupted apply may never have reached this entry.
				// No journal means no permission to restore it. Only an exact
				// untouched original is safely skipped.
				remote, meta, readErr := c.Download(ctx, e.Path)
				if readErr != nil {
					return count, readErr
				}
				if bytes.Equal(remote, before) && meta.Rev == e.Rev {
					continue
				}
			}
			if err != nil {
				return count, errors.New("rollback requires recorded post-repair revision for every entry")
			}
			var s Success
			if json.Unmarshal(raw, &s) != nil || s.Path != e.Path || s.Rev == "" || s.SHA256 != e.After || s.Hash != ContentHash(after) {
				return count, errors.New("invalid success journal")
			}
			rev = s.Rev
			before, after = after, before
		}
		journalPath := filepath.Join(journalDir, e.Path[1:]+".json")
		// A persisted success is checked against remote, not blindly skipped.
		done := false
		for attempt := 0; attempt < 5; attempt++ {
			if err := pause(ctx, 250*time.Millisecond); err != nil {
				return count, err
			}
			raw, remote, err := c.Download(ctx, e.Path)
			if err != nil {
				return count, err
			}
			if bytes.Equal(raw, after) {
				if remote.Rev == rev {
					return count, errors.New("repaired bytes have unchanged original revision")
				}
				success := Success{Path: e.Path, Rev: remote.Rev, Hash: remote.Hash, SHA256: sha(raw)}
				if old, err := os.ReadFile(journalPath); err == nil {
					var recorded Success
					if json.Unmarshal(old, &recorded) != nil || recorded != success {
						return count, errors.New("remote revision differs from recorded success; reconcile")
					}
				} else if !os.IsNotExist(err) {
					return count, err
				} else if err := record(journalPath, success); err != nil {
					return count, err
				}
				if err := syncDir(journalDir); err != nil {
					return count, err
				}
				count++
				done = true
				break
			}
			if !bytes.Equal(raw, before) || remote.Rev != rev {
				// Recompute only to classify a conflict; never adopt a newer rev.
				_, correctionErr := enveloperepair.Correct(raw, e.Path[1:33])
				if correctionErr != nil {
					return count, errors.New("conflicting remote is invalid; no overwrite")
				}
				return count, errors.New("CAS conflict: downloaded/recomputed newer item; prepare a new reviewed manifest, no overwrite")
			}
			if _, err := os.Stat(journalPath); err == nil {
				return count, errors.New("previously repaired item reverted; refusing rewrite")
			}
			if attempt == 4 {
				break // Leave the final iteration for reconciliation only.
			}
			// Whether success, conflict, timeout, or 429, next iteration downloads
			// and reconciles. There is no unconditional overwrite fallback.
			if err := c.update(ctx, e.Path, rev, after); errors.Is(err, errRetryLater) {
				return count, err
			}
		}
		if !done {
			return count, errors.New("conditional upload did not converge within retry bound; resume by reconciliation")
		}
		if count%20 == 0 {
			c.progress("verified journal entries", count)
			if err := pause(ctx, 2*time.Second); err != nil {
				return count, err
			}
		}
	}
	c.progress("verified journal entries", count)
	return count, nil
}
