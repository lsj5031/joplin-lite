package clipper

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// A clipped article can legitimately contain megabytes of image data. Keep
	// each image and the whole note bounded so one bad page cannot exhaust RAM.
	maxInlineImageBytes = 12 << 20
	maxInlineNoteBytes  = 48 << 20
	inlineImageWorkers  = 4
)

var (
	markdownImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\((\S+?)(\s+"[^"]*")?\)`)
	inlineImageClient    = &http.Client{Timeout: 25 * time.Second}
	errImageBudget       = errors.New("inline image budget reached")
)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// inlineClippedImages replaces remote images in a clipped Markdown body with
// their bytes. The web clipper sends article images as URLs; gated URLs and
// offline pages then lose them, so a self-contained note keeps the base64.
// Failed images stay as absolute URLs instead of breaking the clip.
func inlineClippedImages(ctx context.Context, body, baseURL string) string {
	matches := markdownImagePattern.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return body
	}

	type imageRef struct {
		start, end int
		resolved   string
	}
	refs := make([]imageRef, 0, len(matches))
	unique := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		resolved, ok := resolveImageURL(baseURL, body[m[4]:m[5]])
		if !ok {
			continue
		}
		refs = append(refs, imageRef{start: m[4], end: m[5], resolved: resolved})
		unique[resolved] = struct{}{}
	}
	if len(refs) == 0 {
		return body
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	fetched := fetchInlineImages(ctx, unique, baseURL)
	var b strings.Builder
	b.Grow(len(body))
	last := 0
	for _, ref := range refs {
		replacement, ok := fetched[ref.resolved]
		if !ok {
			replacement = ref.resolved
		}
		b.WriteString(body[last:ref.start])
		b.WriteString(replacement)
		last = ref.end
	}
	b.WriteString(body[last:])
	return b.String()
}

// resolveImageURL resolves a Markdown image destination against the page URL.
// Only http(s) images are fetched; data URLs and Joplin resources are already
// self-contained.
func resolveImageURL(baseURL, raw string) (string, bool) {
	ref := strings.TrimSpace(raw)
	if len(ref) >= 2 && ref[0] == '<' && ref[len(ref)-1] == '>' {
		ref = ref[1 : len(ref)-1]
	}
	if ref == "" || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, ":/") {
		return "", false
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", false
	}
	if u.Scheme == "" {
		if baseURL == "" {
			return "", false
		}
		base, err := url.Parse(baseURL)
		if err != nil {
			return "", false
		}
		u = base.ResolveReference(u)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

type imageFetchResult struct {
	url     string
	dataURL string
	size    int
	err     error
}

func fetchInlineImages(ctx context.Context, urls map[string]struct{}, referer string) map[string]string {
	results := make(chan imageFetchResult, len(urls))
	sem := make(chan struct{}, inlineImageWorkers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0

	for u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- imageFetchResult{url: u, err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			mu.Lock()
			overBudget := total >= maxInlineNoteBytes
			mu.Unlock()
			if overBudget {
				results <- imageFetchResult{url: u, err: errImageBudget}
				return
			}

			dataURL, size, err := fetchInlineImage(ctx, u, referer)
			if err == nil {
				mu.Lock()
				if total+size > maxInlineNoteBytes {
					err = errImageBudget
				} else {
					total += size
				}
				mu.Unlock()
			}
			results <- imageFetchResult{url: u, dataURL: dataURL, size: size, err: err}
		}(u)
	}

	wg.Wait()
	close(results)

	out := make(map[string]string, len(urls))
	fetched, failed, bytes := 0, 0, 0
	for r := range results {
		if r.err != nil {
			failed++
			continue
		}
		out[r.url] = r.dataURL
		fetched++
		bytes += r.size
	}
	if fetched > 0 || failed > 0 {
		slog.Info("clipper inline images", "fetched", fetched, "failed", failed, "bytes", bytes)
	}
	return out
}

func fetchInlineImage(ctx context.Context, imageURL, referer string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := inlineImageClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", 0, fmt.Errorf("image status %s", resp.Status)
	}
	if resp.ContentLength > maxInlineImageBytes {
		return "", 0, fmt.Errorf("image too large: %d bytes", resp.ContentLength)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInlineImageBytes+1))
	if err != nil {
		return "", 0, err
	}
	if len(data) > maxInlineImageBytes {
		return "", 0, fmt.Errorf("image too large: over %d bytes", maxInlineImageBytes)
	}
	if len(data) == 0 {
		return "", 0, errors.New("empty image")
	}

	mimeType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if mediaType, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = mediaType
	}
	if mimeType == "image/jpg" {
		mimeType = "image/jpeg"
	}
	if !supportedImageMime(mimeType) {
		mimeType = http.DetectContentType(data)
	}
	if !supportedImageMime(mimeType) {
		return "", 0, fmt.Errorf("unsupported image type %q", mimeType)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return "data:" + mimeType + ";base64," + encoded, len(data), nil
}

func supportedImageMime(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml":
		return true
	default:
		return false
	}
}
