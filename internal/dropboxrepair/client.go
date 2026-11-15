// Package dropboxrepair performs narrowly scoped, revision-conditional envelope
// corrections. It does not use the normal sync engine's overwrite uploader.
package dropboxrepair

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Metadata struct {
	Tag  string `json:".tag"`
	Path string `json:"path_lower"`
	Rev  string `json:"rev,omitempty"`
	Hash string `json:"content_hash,omitempty"`
	Size int64  `json:"size,omitempty"`
}

type Client struct {
	Token        string
	HTTP         *http.Client
	API, Content string
	// Progress receives counts only, never item content or credentials.
	Progress func(stage string, count int)
}

func (c *Client) progress(stage string, count int) {
	if c.Progress != nil {
		c.Progress(stage, count)
	}
}

var errRetryLater = errors.New("Dropbox retry delay exceeds run bound; resume later")

func NewClient(token string) *Client {
	return &Client{Token: token, HTTP: &http.Client{Timeout: 60 * time.Second},
		API: "https://api.dropboxapi.com", Content: "https://content.dropboxapi.com"}
}

// ContentHash is Dropbox's SHA-256 of 4 MiB block SHA-256s, not a file SHA-256.
func ContentHash(data []byte) string {
	h := sha256.New()
	for len(data) > 0 {
		n := min(len(data), 4<<20)
		block := sha256.Sum256(data[:n])
		h.Write(block[:])
		data = data[n:]
	}
	return hex.EncodeToString(h.Sum(nil))
}

func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// request never returns provider bodies or credentials in errors. Uploads are
// not retried here: even a broken response body may follow a committed write.
func (c *Client) request(ctx context.Context, endpoint string, arg any, payload []byte, content, upload bool) ([]byte, Metadata, int, error) {
	encoded, err := json.Marshal(arg)
	if err != nil {
		return nil, Metadata{}, 0, err
	}
	for attempt := 0; attempt < 4; attempt++ {
		base, body := c.API, encoded
		if content {
			base, body = c.Content, payload
		}
		req, err := http.NewRequestWithContext(ctx, "POST", base+endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, Metadata{}, 0, errors.New("invalid Dropbox request")
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Content-Type", "application/json")
		if content {
			req.Header.Set("Dropbox-API-Arg", string(encoded))
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		resp, err := c.HTTP.Do(req)
		status := 0
		var raw []byte
		var meta Metadata
		delay := time.Duration(1<<attempt) * time.Second
		if err == nil {
			status = resp.StatusCode
			// Metadata is small; item metadata may contain large ciphertext.
			raw, err = io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
			resp.Body.Close()
			if len(raw) > 64<<20 {
				return nil, meta, status, errors.New("Dropbox response exceeds safety limit")
			}
			if content && !upload {
				if json.Unmarshal([]byte(resp.Header.Get("Dropbox-API-Result")), &meta) != nil && status == 200 {
					err = errors.New("missing download metadata")
				}
			}
			if status == 429 {
				v := resp.Header.Get("Retry-After")
				if seconds, e := strconv.Atoi(v); e == nil && seconds >= 0 {
					delay = time.Duration(seconds) * time.Second
				} else if date, e := http.ParseTime(v); e == nil {
					delay = max(0, time.Until(date))
				}
				// Do not ignore a long Retry-After by retrying early.
				if delay > 2*time.Minute {
					return nil, meta, status, errRetryLater
				}
			}
		}
		if status == 429 {
			if e := pause(ctx, delay); e != nil {
				return nil, meta, status, e
			}
		}
		if err == nil && status == 200 {
			return raw, meta, status, nil
		}
		if upload || (err == nil && status != 429 && status < 500) || attempt == 3 {
			return nil, meta, status, fmt.Errorf("Dropbox request failed (HTTP %d); no response content logged", status)
		}
		if status != 429 {
			if e := pause(ctx, delay); e != nil {
				return nil, meta, status, e
			}
		}
	}
	panic("unreachable")
}

func (c *Client) Inventory(ctx context.Context) ([]Metadata, string, error) {
	var all []Metadata
	endpoint := "/2/files/list_folder"
	var arg any = map[string]any{"path": "", "recursive": true, "include_deleted": false, "limit": 2000}
	for pages := 0; pages < 10000; pages++ {
		raw, _, _, err := c.request(ctx, endpoint, arg, nil, false, false)
		if err != nil {
			return nil, "", err
		}
		var page struct {
			Entries []Metadata `json:"entries"`
			Cursor  string     `json:"cursor"`
			More    bool       `json:"has_more"`
		}
		if json.Unmarshal(raw, &page) != nil || page.Cursor == "" {
			return nil, "", errors.New("invalid inventory response")
		}
		all = append(all, page.Entries...)
		if !page.More {
			return all, page.Cursor, nil
		}
		endpoint, arg = "/2/files/list_folder/continue", map[string]string{"cursor": page.Cursor}
	}
	return nil, "", errors.New("inventory page limit exceeded")
}

func (c *Client) Download(ctx context.Context, path string) ([]byte, Metadata, error) {
	raw, meta, _, err := c.request(ctx, "/2/files/download", map[string]string{"path": path}, nil, true, false)
	if err != nil {
		return nil, meta, err
	}
	if meta.Path != path || meta.Rev == "" || meta.Hash != ContentHash(raw) || meta.Size != int64(len(raw)) {
		return nil, meta, errors.New("download metadata/hash mismatch")
	}
	// /download returns FileMetadata directly, without list_folder's union
	// discriminator. Compare the same domain value at both boundaries.
	if meta.Tag == "" {
		meta.Tag = "file"
	}
	if meta.Tag != "file" {
		return nil, meta, errors.New("download is not file metadata")
	}
	return raw, meta, nil
}

func (c *Client) update(ctx context.Context, path, rev string, data []byte) error {
	if !itemPath.MatchString(path) || rev == "" {
		return errors.New("invalid conditional upload")
	}
	raw, _, _, err := c.request(ctx, "/2/files/upload", map[string]any{
		"path": path, "mode": map[string]string{".tag": "update", "update": rev},
		"autorename": false, "strict_conflict": true, "mute": true,
	}, data, true, true)
	if err != nil {
		return err
	}
	var m Metadata
	if json.Unmarshal(raw, &m) != nil || m.Path != path || m.Rev == "" || m.Hash != ContentHash(data) {
		return errors.New("upload response metadata mismatch; reconcile download")
	}
	return nil
}
