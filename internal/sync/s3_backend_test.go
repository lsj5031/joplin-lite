//go:build s3

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeS3 is an in-memory s3Objects for driving S3Backend in tests.
type fakeS3 struct {
	data  map[string][]byte
	times map[string]time.Time
}

func newFakeS3() *fakeS3 {
	return &fakeS3{data: map[string][]byte{}, times: map[string]time.Time{}}
}

func (f *fakeS3) GetObject(_ context.Context, key string) ([]byte, error) {
	if v, ok := f.data[key]; ok {
		return append([]byte(nil), v...), nil
	}
	return nil, nil // s3.Client convention: missing -> nil, nil
}

func (f *fakeS3) PutObject(_ context.Context, key string, body []byte) error {
	f.data[key] = append([]byte(nil), body...)
	f.times[key] = time.Now()
	return nil
}

func (f *fakeS3) DeleteObject(_ context.Context, key string) error {
	delete(f.data, key)
	delete(f.times, key)
	return nil
}

func (f *fakeS3) ListObjects(_ context.Context, prefix, continuationToken string, maxKeys int32) ([]string, []int64, string, error) {
	var keys []string
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	offset := 0
	if continuationToken != "" {
		offset, _ = strconv.Atoi(strings.TrimPrefix(continuationToken, "off:"))
	}
	end := offset + int(maxKeys)
	if end > len(keys) {
		end = len(keys)
	}
	page := keys[offset:end]
	times := make([]int64, len(page))
	for i, k := range page {
		times[i] = f.times[k].UnixMilli()
	}
	next := ""
	if end < len(keys) {
		next = "off:" + strconv.Itoa(end)
	}
	return page, times, next, nil
}

func TestS3Backend_Locks(t *testing.T) {
	b := newS3Backend(newFakeS3())
	f := b.client.(*fakeS3)

	// A foreign EXCLUSIVE lock refuses acquisition (the engine then refuses
	// the sync — target 8 is a file-lock target).
	if err := f.PutObject(context.Background(), "locks/2_1_desktop.json", []byte(`{"type":2}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AcquireLock(); !errors.Is(err, ErrExclusiveLock) {
		t.Fatalf("AcquireLock = %v, want ErrExclusiveLock", err)
	}
	if _, ok := f.data["locks/1_3_joplin-lite.json"]; ok {
		t.Fatal("our lock object exists after a refused acquire")
	}
	if err := f.DeleteObject(context.Background(), "locks/2_1_desktop.json"); err != nil {
		t.Fatal(err)
	}

	// Acquire / refresh / release over the lockStore surface.
	lock, err := b.AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if _, ok := f.data["locks/1_3_joplin-lite.json"]; !ok {
		t.Fatalf("lock object not written: %v", f.data)
	}
	if err := b.RefreshLock(lock); err != nil {
		t.Fatalf("RefreshLock: %v", err)
	}
	if err := b.ReleaseLock(lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
	if _, ok := f.data["locks/1_3_joplin-lite.json"]; ok {
		t.Fatal("lock object still present after release")
	}
}

func TestS3Backend_ListingSkipsSystemPaths(t *testing.T) {
	b := newS3Backend(newFakeS3())
	f := b.client.(*fakeS3)
	for _, k := range []string{"aaa.md", ".resource/ccc", "info.json", "locks/1_3_x.json", "temp/scratch"} {
		if err := f.PutObject(context.Background(), k, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}

	data, err := b.Get("/api/items/root:/:/delta")
	if err != nil {
		t.Fatalf("delta: %v", err)
	}
	var delta DeltaResponse
	if err := json.Unmarshal(data, &delta); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, it := range delta.Items {
		names[it.ItemName] = true
	}
	if len(delta.Items) != 2 || !names["aaa.md"] || !names[".resource/ccc"] {
		t.Fatalf("delta items = %v, want only the sync items", names)
	}

	data, err = b.Get("/api/items/root:/:/children")
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	var children ChildrenResponse
	if err := json.Unmarshal(data, &children); err != nil {
		t.Fatal(err)
	}
	names = map[string]bool{}
	for _, c := range children.Items {
		names[c.Name] = true
	}
	if len(children.Items) != 2 || !names["aaa.md"] || !names[".resource/ccc"] {
		t.Fatalf("children = %v, want only the sync items", names)
	}
}
