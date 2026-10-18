package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/lsj5031/joplin-lite/internal/clipper"
	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/store"
	jsync "github.com/lsj5031/joplin-lite/internal/sync"
)

// backend is the in-process clipper server the webview talks to. Keeping it
// in-process (rather than launching `gojoplin serve`) is what makes the
// desktop app a single program with a single lifecycle: closing the window
// closes the database, and there is no port to configure between the UI and
// the API.
type backend struct {
	db      *store.DB
	httpSrv *http.Server
	cancel  context.CancelFunc
	baseURL string
	token   string
	profile string
	once    sync.Once
}

func startBackend(cfgPath, dataDir string, noSync bool, addrOverride string) (*backend, error) {
	cfg, err := config.Load(cfgPath, config.Overrides{NoSync: noSync})
	if err != nil {
		return nil, err
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
	}
	for _, s := range cfg.UnsupportedSubsystems() {
		slog.Warn("configured but not implemented; the setting is ignored", "setting", s)
	}
	cfg.DataDir, err = filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve database directory: %w", err)
	}

	// The desktop UI is the owner of the database, so the window grants write
	// access to the whole vault regardless of the MCP allow-list (which is a
	// restriction for `serve`'s third-party clients). An app that cannot save
	// notes because a clipper policy defaulted to read-only would be a
	// surprising failure, so this is explicit rather than inherited.
	uiCfg := *cfg
	uiCfg.MCPAllowFolders = "*"
	uiCfg.MCPAllowTags = "*"
	uiCfg.MCPAllowCreateFolder = true
	uiCfg.MCPAllowCreateTag = true

	// A configured token keeps the Data API stable across launches (the web
	// clipper pastes it once). Without one, mint an ephemeral token for the
	// page only — never persist it, and never log it.
	token := cfg.APIToken
	if token == "" {
		token, err = randomToken()
		if err != nil {
			return nil, fmt.Errorf("generate api token: %w", err)
		}
		slog.Warn("no api.token configured; using an ephemeral token for this session")
	}

	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	var syncer clipper.SyncTrigger
	var engine *jsync.Engine
	if cfg.NoSync || cfg.SyncTarget == 0 {
		slog.Info("sync disabled: serving the local database only")
	} else {
		remote, err := newSyncBackend(cfg)
		if err != nil {
			cancel()
			db.Close()
			return nil, err
		}
		engine = jsync.NewEngine(remote, db, cfg.MasterPassword)
		syncer = engine
		go syncLoop(ctx, engine)
	}

	// Listen before creating the window so a port collision is a clear dialog
	// (Joplin desktop uses 41184 by default) instead of a blank page.
	addr := cfg.ListenAddr()
	if addrOverride != "" {
		addr = addrOverride
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		cancel()
		db.Close()
		return nil, fmt.Errorf("cannot start the local API on %s: %w\n\nJoplin desktop uses port 41184 by default. "+
			"Close it or set server.port in the config.", addr, err)
	}

	srv := clipper.NewServer(db, token, cfg.APIKey, syncer, clipper.NewPolicy(&uiCfg), authPrompt{})
	httpSrv := &http.Server{Handler: srv.Router()}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("local API stopped", "error", err)
		}
	}()

	base := baseURLFor(ln.Addr())
	slog.Info("desktop API listening", "base", base, "data_dir", cfg.DataDir)

	return &backend{
		db:      db,
		httpSrv: httpSrv,
		cancel:  cancel,
		baseURL: base,
		token:   token,
		profile: fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.DataDir))),
	}, nil
}

// baseURLFor turns the bound address into a URL the webview can fetch. A
// wildcard bind is reachable at loopback.
func baseURLFor(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if host == "" || host == "::" || host == "0.0.0.0" || host == "::1" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// newSyncBackend selects the configured remote. The S3 backend is compiled
// out unless the binary is built with -tags s3, and its stub explains that.
func newSyncBackend(cfg *config.Config) (jsync.SyncBackend, error) {
	if cfg.SyncTarget == 8 {
		b, err := jsync.NewS3Backend(cfg)
		if err != nil {
			return nil, fmt.Errorf("S3 backend: %w", err)
		}
		return jsync.NewTracedBackend(b, "s3"), nil
	}
	c, err := jsync.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return jsync.NewTracedBackend(c, "joplin_server"), nil
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// syncLoop mirrors `serve`: initial sync, then every 10 minutes or when a
// UI mutation asks for one.
func syncLoop(ctx context.Context, engine *jsync.Engine) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	if err := engine.Sync(ctx); err != nil {
		slog.Error("initial sync failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := engine.Sync(ctx); err != nil {
				slog.Error("sync failed", "error", err)
			}
		case <-engine.TriggerCh():
			if err := engine.Sync(ctx); err != nil {
				slog.Error("triggered sync failed", "error", err)
			}
		}
	}
}

func (b *backend) Close() {
	b.once.Do(func() {
		b.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = b.httpSrv.Shutdown(ctx)
		if b.db != nil {
			_ = b.db.Close()
		}
	})
}

// authPrompt is the native Accept/Reject surface for web-clipper pairing.
// It defaults to Reject: pairing is never accepted without a person clicking.
type authPrompt struct{}

func (authPrompt) PromptAuth(authToken string) bool {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:          mygo.MessageQuestion,
		Title:         "Web clipper pairing",
		Message:       "A browser extension wants to connect to Joplin Lite.",
		Detail:        "Accept only if you just started pairing the Joplin web clipper.",
		Buttons:       []string{"Reject", "Accept"},
		DefaultButton: 0,
		CancelButton:  0,
	})
	return err == nil && res.Button == 1
}
