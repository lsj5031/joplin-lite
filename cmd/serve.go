package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lsj5031/joplin-lite/internal/clipper"
	"github.com/lsj5031/joplin-lite/internal/store"
	"github.com/lsj5031/joplin-lite/internal/sync"
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
			if cfg.SyncTarget == 8 {
				b, err := sync.NewS3Backend(cfg)
				if err != nil {
					return fmt.Errorf("S3 backend: %w", err)
				}
				backend = sync.NewTracedBackend(b, "s3")
			} else {
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

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigCh
			slog.Info("shutting down")
			cancel()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			httpSrv.Shutdown(shutdownCtx)
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

func init() {
	serveCmd.Flags().BoolVar(&noSync, "no-sync", false, "serve the local database without a sync remote (no background sync)")
	rootCmd.AddCommand(serveCmd)
}
