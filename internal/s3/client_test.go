//go:build s3

package s3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// missingObjectServer answers like an S3-compatible store: a missing key is a
// 404 whose body carries the error code, and a missing bucket is a different
// 404 that must not be mistaken for "no objects yet".
func missingObjectServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bucket/info.json":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`))
		case "/bucket/note.md":
			w.Write([]byte("payload"))
		case "/bucket/denied.md":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>no</Message></Error>`))
		case "/bucket/nobucket.md":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchBucket</Code><Message>no bucket</Message></Error>`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>gone</Message></Error>`))
		}
	}))
}

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	c, err := NewClient(context.Background(), Config{
		Bucket:          "bucket",
		Region:          "us-east-1",
		Endpoint:        endpoint,
		ForcePathStyle:  true,
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// A missing object is reported as nil, nil: the backends treat that as "not on
// the target yet" (info.json on a fresh bucket) and the S3 store may return
// either a modeled code or a bare 404.
func TestClientGetObject_MissingKeyReturnsNil(t *testing.T) {
	srv := missingObjectServer(t)
	defer srv.Close()
	c := testClient(t, srv.URL)

	data, err := c.GetObject(context.Background(), "info.json")
	if err != nil || data != nil {
		t.Fatalf("GetObject(missing) = (%q, %v), want nil, nil", data, err)
	}
}

func TestClientGetObject_ExistingKeyReturnsBytes(t *testing.T) {
	srv := missingObjectServer(t)
	defer srv.Close()
	c := testClient(t, srv.URL)

	data, err := c.GetObject(context.Background(), "note.md")
	if err != nil || string(data) != "payload" {
		t.Fatalf("GetObject = (%q, %v), want payload", data, err)
	}
}

func TestClientGetObject_ErrorsStayErrors(t *testing.T) {
	srv := missingObjectServer(t)
	defer srv.Close()
	c := testClient(t, srv.URL)

	if _, err := c.GetObject(context.Background(), "denied.md"); err == nil {
		t.Error("GetObject(403) returned no error")
	}
	// A wrong bucket name is a configuration error, not an empty target: it
	// must fail instead of looking like "no items to sync".
	if _, err := c.GetObject(context.Background(), "nobucket.md"); err == nil {
		t.Error("GetObject(NoSuchBucket) returned no error")
	}
}

func TestClientHeadObject_MissingKeyIsFalse(t *testing.T) {
	srv := missingObjectServer(t)
	defer srv.Close()
	c := testClient(t, srv.URL)

	ok, err := c.HeadObject(context.Background(), "info.json")
	if err != nil || ok {
		t.Fatalf("HeadObject(missing) = (%v, %v), want false, nil", ok, err)
	}
	ok, err = c.HeadObject(context.Background(), "note.md")
	if err != nil || !ok {
		t.Fatalf("HeadObject(existing) = (%v, %v), want true, nil", ok, err)
	}
}
