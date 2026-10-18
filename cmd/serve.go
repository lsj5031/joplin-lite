package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lsj5031/joplin-lite/internal/clipper"
	"github.com/lsj5031/joplin-lite/internal/store"
	"github.com/lsj5031/joplin-lite/internal/sync"
	"github.com/lsj5031/joplin-lite/internal/webui"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the clipper server with background sync",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if cfg.APIToken == "" {
			return fmt.Errorf("api token is required: set api.token in config (e.g. ${GOJOPLIN_API_TOKEN}) or GOJOPLIN_API_TOKEN env var")
		}
		if cfg.APIKey == "" {
			return fmt.Errorf("api key is required: set api.key in config (e.g. ${GOJOPLIN_API_KEY}) or GOJOPLIN_API_KEY env var")
		}
		for _, s := range cfg.UnsupportedSubsystems() {
			slog.Warn("configured but not implemented; the setting is ignored", "setting", s)
		}

		db, err := store.Open(cfg.DataDir)
		if err != nil {
			return err
		}
		defer db.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// syncer stays nil in local-only mode: the server is read/write against
		// the local database, it just never talks to a remote.
		var syncer clipper.SyncTrigger
		if cfg.NoSync {
			slog.Info("sync disabled (--no-sync): serving the local database only")
		} else {
			var backend sync.SyncBackend
			switch cfg.SyncTarget {
			case 2:
				backend = sync.NewTracedBackend(sync.NewLocalFolderBackend(cfg.LocalDir), "local")
			case 7:
				b, err := sync.NewDropboxBackend(cfg)
				if err != nil {
					return fmt.Errorf("Dropbox backend: %w", err)
				}
				backend = sync.NewTracedBackend(b, "dropbox")
			case 8:
				b, err := sync.NewS3Backend(cfg)
				if err != nil {
					return fmt.Errorf("S3 backend: %w", err)
				}
				backend = sync.NewTracedBackend(b, "s3")
			default:
				c, err := sync.NewClient(cfg)
				if err != nil {
					return err
				}
				backend = sync.NewTracedBackend(c, "joplin_server")
			}

			engine := sync.NewEngine(backend, db, cfg.MasterPassword)
			syncer = engine

			go func() {
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
			}()
		}

		policy := clipper.NewPolicy(cfg)
		srv := clipper.NewServer(db, cfg.APIToken, cfg.APIKey, syncer, policy, consoleAuthPrompt{out: os.Stdout})
		addr := cfg.ListenAddr()
		slog.Info("starting clipper server", "addr", addr)
		slog.Info("clipper API token (paste into the extension or settings)", "token", cfg.APIToken)

		httpSrv := &http.Server{
			Addr:    addr,
			Handler: srv.Router(),
		}

		// Optional browser preview: the web build is no longer embedded in any
		// binary, but serving the same vault's SPA from disk keeps the web UI
		// usable without a Node dev server. It gets its own listener because
		// the Data API owns the top-level paths (`/notes`, `/tags`, ...).
		var webSrv *http.Server
		if dir := webDirPath(); dir != "" {
			handler, err := webui.New(os.DirFS(dir), webui.RuntimeConfig{
				Base:    apiBase(cfg.ListenAddr()),
				Token:   cfg.APIToken,
				Shell:   "browser",
				Profile: fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.DataDir))),
			})
			if err != nil {
				return err
			}
			webSrv = &http.Server{Addr: webAddr, Handler: handler}
			go func() {
				slog.Info("web preview listening", "addr", webAddr, "dir", dir, "api", apiBase(cfg.ListenAddr()))
				if err := webSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					slog.Error("web preview stopped", "error", err)
				}
			}()
		}

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigCh
			slog.Info("shutting down")
			cancel()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			httpSrv.Shutdown(shutdownCtx)
			if webSrv != nil {
				webSrv.Shutdown(shutdownCtx)
			}
		}()

		if err := clipper.Serve(httpSrv); err != nil {
			return err
		}
		return nil
	},
}

// consoleAuthPrompt is the M3 Accept/Reject surface for clipper pairing
// (Joplin shows a native dialog). The M4 UI replaces it via Server.ResolveAuth.
type consoleAuthPrompt struct {
	out io.Writer
}

func (p consoleAuthPrompt) PromptAuth(authToken string) bool {
	fmt.Fprintf(p.out, "\nThe web clipper is asking to pair (auth token %s).\nAccept the clipper? [y/N]: ", authToken)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false // no stdin — reject rather than auto-accept
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// webDirPath returns the SPA directory to serve, from --web-dir or
// GOJOPLIN_WEB_DIR.
func webDirPath() string {
	if webDir != "" {
		return webDir
	}
	return strings.TrimSpace(os.Getenv("GOJOPLIN_WEB_DIR"))
}

// apiBase turns the API listen address into a URL a browser can call. A
// wildcard bind is reachable at loopback.
func apiBase(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	switch host {
	case "", "0.0.0.0", "::", "::1":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

var (
	webDir  string
	webAddr string
)

func init() {
	serveCmd.Flags().BoolVar(&noSync, "no-sync", false, "serve the local database without a sync remote (no background sync)")
	serveCmd.Flags().StringVar(&webDir, "web-dir", "", "serve the built SPA (web/dist/client) as a browser preview (env: GOJOPLIN_WEB_DIR)")
	serveCmd.Flags().StringVar(&webAddr, "web-addr", "127.0.0.1:41185", "listen address for the --web-dir preview")
	rootCmd.AddCommand(serveCmd)
}
