// Package webui serves the built SPA (web/dist/client) as an optional browser
// preview of the same vault. The desktop app no longer embeds the SPA — the
// native window is the product shell — so this handler is only mounted by
// `gojoplin serve --web-dir`, which keeps the web build usable in a browser
// without putting Chromium back in the desktop binary.
package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// RuntimeConfig is injected into the page before the app bundle runs. The API
// listens on a port chosen at launch, so baking it into the build would make
// the preview depend on the machine's free ports.
type RuntimeConfig struct {
	Base    string `json:"base"`
	Token   string `json:"token"`
	Shell   string `json:"shell"`
	Profile string `json:"profile"`
}

// New returns the SPA handler: index.html with the runtime config injected
// before </head>, immutable caching for hashed assets, and an index fallback
// for client-side routes. Missing assets with an extension still 404.
func New(fsys fs.FS, rt RuntimeConfig) (http.Handler, error) {
	payload, err := json.Marshal(rt)
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, fmt.Errorf("frontend index.html (run cd web && npm run build): %w", err)
	}
	if !bytes.Contains(index, []byte("</head>")) {
		return nil, fmt.Errorf("frontend index.html has no </head> to inject the runtime config")
	}
	// encoding/json escapes <, > and & so the payload cannot close the tag.
	script := []byte("<script>window.__GOJOPLIN__=" + string(payload) + ";</script>")
	index = bytes.Replace(index, []byte("</head>"), append(script, []byte("</head>")...), 1)

	files := http.FileServerFS(fsys)
	serveIndex := func(w http.ResponseWriter, head bool) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(index)))
		if !head {
			_, _ = w.Write(index)
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		head := r.Method == http.MethodHead
		if name == "" || name == "." {
			serveIndex(w, head)
			return
		}
		fi, err := fs.Stat(fsys, name)
		if err != nil || fi.IsDir() {
			// Client-side routes have no extension; real missing assets 404.
			if path.Ext(name) == "" {
				serveIndex(w, head)
				return
			}
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	}), nil
}
