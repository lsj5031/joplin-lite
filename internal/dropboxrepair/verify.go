package dropboxrepair

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

type Difference struct {
	Path   string    `json:"path"`
	Before *Metadata `json:"before,omitempty"`
	After  *Metadata `json:"after,omitempty"`
}

type Verification struct {
	Verified  int          `json:"verified_repairs"`
	Affected  int          `json:"remaining_affected"`
	Unrelated []Difference `json:"unrelated_inventory_changes"`
}

// Verify has no remote writes. A new stable full scan must have zero affected
// items. Report concurrent unrelated changes, never revert or conceal them.
func Verify(ctx context.Context, c *Client, backup, scan string, expectedKeys []string) (*Verification, error) {
	m, err := Load(backup)
	if err != nil {
		return nil, err
	}
	current, err := Prepare(ctx, c, scan, expectedKeys)
	if err != nil {
		return nil, err
	}
	result := &Verification{Affected: len(current.Entries)}
	candidates := map[string]bool{}
	for _, e := range m.Entries {
		candidates[e.Path] = true
		_, after, err := original(backup, e)
		if err != nil {
			return nil, err
		}
		raw, meta, err := c.Download(ctx, e.Path)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(raw, after) || meta.Rev == e.Rev {
			return nil, errors.New("remote repair bytes/revision verification failed")
		}
		journal, err := os.ReadFile(filepath.Join(backup, "success", e.Path[1:]+".json"))
		if err != nil {
			return nil, err
		}
		var success Success
		if json.Unmarshal(journal, &success) != nil || success.Path != e.Path || success.Rev != meta.Rev || success.Hash != meta.Hash || success.SHA256 != sha(raw) {
			return nil, errors.New("success journal does not match remote repair")
		}
		result.Verified++
	}
	before, after := map[string]Metadata{}, map[string]Metadata{}
	for _, meta := range m.Inventory {
		before[meta.Path] = meta
	}
	for _, meta := range current.Inventory {
		after[meta.Path] = meta
	}
	paths := map[string]bool{}
	for p := range before {
		paths[p] = true
	}
	for p := range after {
		paths[p] = true
	}
	for p := range paths {
		if candidates[p] {
			continue
		}
		b, bok := before[p]
		a, aok := after[p]
		if bok == aok && b == a {
			continue
		}
		d := Difference{Path: p}
		if bok {
			d.Before = &b
		}
		if aok {
			d.After = &a
		}
		result.Unrelated = append(result.Unrelated, d)
	}
	sort.Slice(result.Unrelated, func(i, j int) bool { return result.Unrelated[i].Path < result.Unrelated[j].Path })
	if err := saveJSON(filepath.Join(scan, "verification.json"), result); err != nil {
		return nil, err
	}
	if err := syncDir(scan); err != nil {
		return nil, err
	}
	if result.Affected != 0 {
		return result, errors.New("stable scan still contains affected envelopes")
	}
	return result, nil
}
