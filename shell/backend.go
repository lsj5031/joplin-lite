package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	// cfg is the effective configuration: the file plus the native settings
	// overlay. Settings changes replace it and the sync loop behind syncMu.
	cfg *config.Config
	srv *clipper.Server

	// cncl stops the whole backend; the loop's own cancel only stops one
	// generation of it, so a target change can restart sync without reopening
	// the database or the API.
	cncl context.Context

	// syncMu guards the loop handles below. They are read from the UI poll and
	// replaced from the settings dialog, so they cannot be plain fields.
	syncMu     sync.Mutex
	engine     *jsync.Engine
	syncCancel context.CancelFunc
	syncDone   chan struct{}
	syncGen    int
	closed     bool

	status *syncStatus
	notify func()
	launch launchSettings

	// logRestore closes the desktop log file and puts the previous default
	// logger back; nil for a backend that never opened one.
	logRestore func()
}

// syncFailure classifies a failed pass. The footer can then say what happened
// (another device holds the lock, the connection is gone) instead of showing
// a raw Go error, and only failures that may clear on their own are retried.
type syncFailure string

const (
	syncFailureNone    syncFailure = ""
	syncFailureLock    syncFailure = "lock"
	syncFailureOffline syncFailure = "offline"
	syncFailureTarget  syncFailure = "target"
	syncFailureOther   syncFailure = "other"
)

// syncStatus is the current/last sync state, read by the native window from
// the UI thread while syncLoop writes it from its own goroutine.
type syncStatus struct {
	mu       sync.Mutex
	running  bool
	pending  bool
	phase    string
	revision uint64
	last     time.Time
	err      string
	kind     syncFailure
	retry    time.Time
	persist  func(time.Time) error
	notify   func()
}

// syncSnapshot is a consistent copy of the sync state.
type syncSnapshot struct {
	Enabled  bool
	Running  bool
	Pending  bool
	Phase    string
	Revision uint64
	Last     time.Time
	Err      string
	Kind     syncFailure
	// Retry is when a failed pass will be tried again on its own; zero when
	// the next attempt waits for the timer or the user.
	Retry time.Time
}

func (s *syncStatus) setNotify(fn func()) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.notify = fn
	s.mu.Unlock()
}

func (s *syncStatus) wake() {
	if s == nil {
		return
	}
	s.mu.Lock()
	fn := s.notify
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// reset clears the last result when a new sync engine (a new target) replaces
// the old one, so the footer does not show the previous target's failure.
func (s *syncStatus) reset() {
	s.mu.Lock()
	s.running, s.pending, s.last, s.err = false, false, time.Time{}, ""
	s.phase = ""
	s.kind, s.retry = syncFailureNone, time.Time{}
	s.mu.Unlock()
	s.wake()
}

func (s *syncStatus) request() {
	s.mu.Lock()
	s.pending = true
	s.mu.Unlock()
	s.wake()
}

func (s *syncStatus) start() {
	s.mu.Lock()
	s.pending, s.running = false, true
	s.phase = ""
	s.retry = time.Time{}
	s.mu.Unlock()
	s.wake()
}

func (s *syncStatus) progress(phase string) {
	s.mu.Lock()
	changed := s.phase != phase
	s.phase = phase
	s.mu.Unlock()
	if changed {
		s.wake()
	}
}

func (s *syncStatus) finish(err error) {
	s.mu.Lock()
	s.running, s.pending, s.phase = false, false, ""
	s.revision++
	s.kind, s.retry = syncFailureFor(err), time.Time{}
	if err != nil {
		s.err = err.Error()
	} else {
		s.err = ""
		s.last = time.Now()
		if s.persist != nil {
			if err := s.persist(s.last); err != nil {
				slog.Warn("cannot persist last successful sync time")
			}
		}
	}
	s.mu.Unlock()
	s.wake()
}

// retryIn records the automatic retry the loop has armed.
func (s *syncStatus) retryIn(at time.Time) {
	s.mu.Lock()
	s.retry = at
	s.mu.Unlock()
	s.wake()
}

func (s *syncStatus) snapshot(enabled bool) syncSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return syncSnapshot{
		Enabled:  enabled,
		Running:  s.running,
		Pending:  s.pending,
		Phase:    s.phase,
		Revision: s.revision,
		Last:     s.last,
		Err:      s.err,
		Kind:     s.kind,
		Retry:    s.retry,
	}
}

func startBackend(cfgPath, dataDir string, noSync bool, addrOverride string, launches ...launchSettings) (_ *backend, err error) {
	var launch launchSettings
	if len(launches) != 0 {
		launch = launches[0]
	}
	// Official settings.json omits secure credentials and the public app key.
	// Defer remote validation until the explicit recovery below supplies them.
	cfg, err := config.Load(cfgPath, config.Overrides{NoSync: noSync || launch.ReuseOfficialKeychain})
	if err != nil {
		return nil, err
	}
	if launch.ReuseOfficialKeychain {
		cfg.NoSync = noSync
		switch strings.ToLower(strings.TrimSpace(os.Getenv("GOJOPLIN_NO_SYNC"))) {
		case "1", "true", "yes", "on":
			cfg.NoSync = true
		}
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
	if launch.ReuseOfficialKeychain {
		keychain := launch.keychain
		if keychain == nil {
			keychain = officialKeychainPassword
		}
		if err := recoverOfficialCredentials(cfg, keychain); err != nil {
			return nil, err
		}
	}
	if launch.DropboxBlockDeletes {
		cfg.DropboxBlockDeletes = true
	}

	// Log to the data directory from here on, so a launch without a console
	// leaves a record of the migrations, the API port and the sync passes. A
	// failure below closes it again rather than leaving a half-started app
	// holding the file.
	logRestore, err := setupDesktopLog(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			logRestore()
		}
	}()
	slog.Info("desktop app starting", "data_dir", cfg.DataDir, "log", filepath.Join(cfg.DataDir, desktopLogName))

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

	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	// The native settings dialog owns a small overlay in key_values. It is
	// applied before the token and the sync engine are built, so a saved
	// target is what this launch uses.
	if stored, ok, err := loadStoredSettings(db); err != nil {
		db.Close()
		return nil, err
	} else if ok {
		if launch.ReuseOfficialKeychain && (stored.Target != 7 || stored.MasterPassword != "") {
			db.Close()
			return nil, errors.New("stored native settings conflict with official Keychain credentials")
		}
		master := cfg.MasterPassword
		overlayStoredSettings(cfg, stored)
		if launch.ReuseOfficialKeychain {
			cfg.MasterPassword = master
		}
	}

	// A configured token keeps the Data API stable across launches (the web
	// clipper pastes it once). Without one, mint an ephemeral token for the
	// page only — never persist it, and never log it.
	token := cfg.APIToken
	if token == "" {
		token, err = randomToken()
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("generate api token: %w", err)
		}
		cfg.APIToken = token
		slog.Warn("no api.token configured; using an ephemeral token for this session")
	}

	// Build the remote before anything is started so a bad target is a startup
	// error (and a settings-save error), not a loop that fails later.
	var remote jsync.SyncBackend
	if !cfg.NoSync && cfg.SyncTarget != 0 {
		remote, err = newSyncBackend(cfg, launch.ReuseOfficialKeychain)
		if err != nil {
			db.Close()
			return nil, err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	status := &syncStatus{}

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

	b := &backend{
		db:         db,
		cancel:     cancel,
		baseURL:    "",
		token:      token,
		profile:    fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.DataDir))),
		cfg:        cfg,
		cncl:       ctx,
		status:     status,
		launch:     launch,
		logRestore: logRestore,
	}

	srv := clipper.NewServer(db, token, cfg.APIKey, b, clipper.NewPolicy(&uiCfg), authPrompt{})
	b.srv = srv
	httpSrv := &http.Server{Handler: srv.Router()}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("local API stopped", "error", err)
		}
	}()

	base := baseURLFor(ln.Addr())
	b.httpSrv = httpSrv
	b.baseURL = base
	slog.Info("desktop API listening", "base", base, "data_dir", cfg.DataDir)

	b.syncMu.Lock()
	startErr := b.startSyncLocked(remote)
	b.syncMu.Unlock()
	if startErr != nil {
		cancel()
		_ = httpSrv.Close()
		db.Close()
		return nil, startErr
	}

	return b, nil
}

// overlayStoredSettings applies a saved dialog state over the loaded config.
// A -no-sync run stays local, but the token and master password still apply so
// they survive it.
func overlayStoredSettings(cfg *config.Config, s desktopSettings) {
	if !cfg.NoSync {
		s.applyTo(cfg)
		return
	}
	cfg.MasterPassword = s.MasterPassword
	if s.APIToken != "" {
		cfg.APIToken = s.APIToken
	}
}

// startSyncLocked starts the background loop. The caller holds syncMu (or is
// still constructing the backend). A nil remote means "no sync".
func (b *backend) startSyncLocked(remote jsync.SyncBackend) error {
	b.status.reset()
	key := syncSuccessKey(b.cfg, b.launch.ReuseOfficialKeychain)
	raw, err := b.db.GetSetting(key)
	if err != nil {
		return fmt.Errorf("load last successful sync time: %w", err)
	}
	last, _ := time.Parse(time.RFC3339Nano, raw)
	b.status.mu.Lock()
	b.status.last = last.Local()
	b.status.persist = func(at time.Time) error { return b.db.SetSetting(key, at.UTC().Format(time.RFC3339Nano)) }
	b.status.mu.Unlock()
	if remote == nil {
		b.engine = nil
		slog.Info("sync disabled: serving the local database only")
		return nil
	}
	ctx, cancel := context.WithCancel(b.cncl)
	engine := jsync.NewEngine(remote, b.db, b.cfg.MasterPassword)
	engine.OnProgress = b.status.progress
	status := b.status
	done := make(chan struct{})
	b.engine, b.syncCancel, b.syncDone = engine, cancel, done
	go func() {
		defer close(done)
		syncLoop(ctx, engine, status)
	}()
	return nil
}

// Settings returns the settings in effect, for the native dialog.
func (b *backend) Settings() desktopSettings {
	if b == nil {
		return desktopSettings{}
	}
	if b.cfg == nil {
		return desktopSettings{APIToken: b.token}
	}
	b.syncMu.Lock()
	cfg := *b.cfg
	if b.launch.ReuseOfficialKeychain {
		cfg.MasterPassword = ""
		cfg.NoSync = false // Keep the protected target visible even on a local-only run.
	}
	b.syncMu.Unlock()
	return settingsFromConfig(&cfg)
}

// ApplySettings validates and persists the dialog's values, then applies them
// without restarting the app: the clipper token changes in place and the sync
// loop is replaced once the previous pass has exited, so two engines never run
// against one target.
func (b *backend) ApplySettings(s desktopSettings) error {
	if b == nil {
		return errors.New("settings are read-only in this build")
	}
	if err := s.validate(); err != nil {
		return err
	}
	b.syncMu.Lock()
	if b.cfg == nil {
		b.syncMu.Unlock()
		return errors.New("settings are read-only in this build")
	}
	cfg := *b.cfg
	b.syncMu.Unlock()
	if b.launch.ReuseOfficialKeychain && (s.Target != 7 || s.MasterPassword != "") {
		return errors.New("official Keychain mode keeps the Dropbox target and its master password; edit launch settings to change them")
	}
	master := cfg.MasterPassword
	disabled := cfg.NoSync
	s.applyTo(&cfg)
	if b.launch.ReuseOfficialKeychain {
		cfg.MasterPassword = master
		cfg.NoSync = disabled
	}
	if b.launch.DropboxBlockDeletes {
		cfg.DropboxBlockDeletes = true
	}

	var remote jsync.SyncBackend
	if !cfg.NoSync && cfg.SyncTarget != 0 {
		var err error
		remote, err = newSyncBackend(&cfg, b.launch.ReuseOfficialKeychain)
		if err != nil {
			return fmt.Errorf("cannot use this sync target: %w", err)
		}
	}
	if err := s.save(b.db); err != nil {
		return err
	}

	b.syncMu.Lock()
	prevDone, prevCancel := b.syncDone, b.syncCancel
	if prevCancel != nil {
		prevCancel()
	}
	b.syncCancel, b.syncDone = nil, nil
	b.engine = nil
	// A finishing old pass may still persist its own target's timestamp, but
	// cannot publish that result as the newly selected target's status.
	b.status = &syncStatus{notify: b.notify}
	b.cfg = &cfg
	b.token = cfg.APIToken
	b.syncGen++
	gen := b.syncGen
	b.syncMu.Unlock()

	b.srv.SetAPIToken(cfg.APIToken)

	go func() {
		if prevDone != nil {
			<-prevDone
		}
		b.syncMu.Lock()
		defer b.syncMu.Unlock()
		// A newer save (or Close) wins; this generation must not start.
		if b.closed || b.syncGen != gen {
			return
		}
		if err := b.startSyncLocked(remote); err != nil {
			b.status.finish(err)
			slog.Error("cannot start sync after settings change", "error", err)
		}
	}()
	return nil
}

// SyncNow asks the sync loop to run immediately. It reports false when the
// instance was started without a remote (-no-sync or no target).
func (b *backend) SyncNow() bool {
	if b == nil {
		return false
	}
	b.syncMu.Lock()
	engine := b.engine
	status := b.status
	b.syncMu.Unlock()
	if engine == nil || status == nil {
		return false
	}
	status.request()
	engine.TriggerSync()
	return true
}

// TriggerSync satisfies clipper.SyncTrigger: a mutation queues a pass.
func (b *backend) TriggerSync() { b.SyncNow() }

// setNotify registers a callback invoked whenever sync state changes.
func (b *backend) setNotify(fn func()) {
	if b == nil {
		return
	}
	b.syncMu.Lock()
	b.notify = fn
	status := b.status
	b.syncMu.Unlock()
	if status != nil {
		status.setNotify(fn)
	}
}

// SyncState returns a consistent snapshot of the sync state for the UI. Sync
// counts as enabled whenever the effective config has a target, even during
// the short window where a settings change has no engine yet.
func (b *backend) SyncState() syncSnapshot {
	if b == nil {
		return syncSnapshot{}
	}
	b.syncMu.Lock()
	enabled := b.cfg != nil && !b.cfg.NoSync && b.cfg.SyncTarget != 0
	status := b.status
	b.syncMu.Unlock()
	if status == nil {
		return syncSnapshot{}
	}
	return status.snapshot(enabled)
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
func newSyncBackend(cfg *config.Config, memoryOnly ...bool) (jsync.SyncBackend, error) {
	switch cfg.SyncTarget {
	case 2:
		return jsync.NewTracedBackend(jsync.NewLocalFolderBackend(cfg.LocalDir), "local"), nil
	case 7:
		if len(memoryOnly) != 0 && memoryOnly[0] {
			copy := *cfg
			copy.DataDir = "" // Ignore token cache; recovered credentials stay in memory.
			cfg = &copy
		}
		b, err := jsync.NewDropboxBackend(cfg)
		if err != nil {
			return nil, fmt.Errorf("Dropbox backend: %w", err)
		}
		return jsync.NewTracedBackend(b, "dropbox"), nil
	case 8:
		b, err := jsync.NewS3Backend(cfg)
		if err != nil {
			return nil, fmt.Errorf("S3 backend: %w", err)
		}
		return jsync.NewTracedBackend(b, "s3"), nil
	default:
		c, err := jsync.NewClient(cfg)
		if err != nil {
			return nil, err
		}
		return jsync.NewTracedBackend(c, "joplin_server"), nil
	}
}

// Only a hash of the effective remote identity is stored in the local profile.
// Tokens are a conservative account boundary when no account ID is available.
func syncSuccessKey(cfg *config.Config, memoryOnly bool) string {
	var identity any
	switch cfg.SyncTarget {
	case 2:
		path, _ := filepath.Abs(cfg.LocalDir)
		identity = []any{2, path}
	case 7:
		dir := cfg.DataDir
		if memoryOnly {
			dir = ""
		}
		token, err := jsync.LoadDropboxToken(dir, cfg.DropboxAuth)
		if err != nil || token == nil {
			identity = []any{7, "unavailable"}
		} else {
			account := token.AccountID
			if account == "" {
				account = token.RefreshToken
			}
			if account == "" {
				account = token.AccessToken
			}
			identity = []any{7, account, cfg.DropboxAppKey, cfg.DropboxBlockDeletes}
		}
	case 8:
		identity = []any{8, cfg.S3URL, cfg.S3Bucket, cfg.S3Region, cfg.S3AccessKey, cfg.S3ForcePathStyle}
	default:
		identity = []any{cfg.SyncTarget, cfg.ServerURL, cfg.Username}
	}
	raw, _ := json.Marshal(identity)
	return fmt.Sprintf("desktop.sync.last_success.%x", sha256.Sum256(raw))
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// syncLoop mirrors `serve`: initial sync, then every 10 minutes or when a
// UI mutation asks for one. The status is recorded around every pass so the
// native window can show progress and refresh when a pass finishes.
//
// A pass that failed for a reason that may clear (a lock another device will
// release, a connection that dropped) is retried sooner, so the app comes back
// on its own when the network returns instead of waiting for the ten-minute
// tick. Failures that need the user keep waiting for that tick.
func syncLoop(ctx context.Context, engine syncer, status *syncStatus) {
	run := func() time.Duration {
		status.start()
		err := engine.Sync(ctx)
		status.finish(err)
		if err == nil {
			return 0
		}
		slog.Error("sync failed", "error", err)
		if d := retryAfter(err); d > 0 {
			status.retryIn(time.Now().Add(d))
			return d
		}
		return 0
	}
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	retry := time.NewTimer(time.Hour)
	if !retry.Stop() {
		<-retry.C
	}
	defer retry.Stop()
	// arm runs a pass and starts or stops the retry timer to match its result.
	arm := func() {
		if !retry.Stop() {
			select {
			case <-retry.C:
			default:
			}
		}
		if d := run(); d > 0 {
			retry.Reset(d)
		}
	}
	arm()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			arm()
		case <-engine.TriggerCh():
			arm()
		case <-retry.C:
			arm()
		}
	}
}

// syncer is the part of jsync.Engine the loop drives, so the retry timing can
// be tested without a sync target.
type syncer interface {
	Sync(ctx context.Context) error
	TriggerCh() <-chan struct{}
}

// Delays for failures that may clear; variables so tests can shorten them.
var (
	// syncRetryLock is short enough that the app takes over soon after the
	// other device finishes, and long enough not to fight it for the lock.
	syncRetryLock = 45 * time.Second
	// syncRetryNetwork lets a flaky connection come back without hammering
	// the target.
	syncRetryNetwork = 30 * time.Second
)

// syncFailureFor says which kind of failure a pass hit.
func syncFailureFor(err error) syncFailure {
	switch {
	case err == nil:
		return syncFailureNone
	case errors.Is(err, jsync.ErrExclusiveLock):
		return syncFailureLock
	case errors.Is(err, jsync.ErrSyncTargetUnsupported):
		return syncFailureTarget
	case isOffline(err):
		return syncFailureOffline
	}
	return syncFailureOther
}

// retryAfter is how long to wait before trying a failed pass again, or zero
// when the failure needs the user and should not be retried behind their back.
func retryAfter(err error) time.Duration {
	switch syncFailureFor(err) {
	case syncFailureLock:
		return syncRetryLock
	case syncFailureOffline:
		return syncRetryNetwork
	}
	return 0
}

// isOffline reports whether a sync error means the target could not be
// reached, rather than refusing the request. HTTP clients usually wrap a
// net.Error; the text check covers the errors built from a response body.
func isOffline(err error) bool {
	var nerr net.Error
	if errors.As(err, &nerr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, hint := range []string{
		"no such host",
		"connection refused",
		"connection reset",
		"network is unreachable",
		"i/o timeout",
		"tls handshake timeout",
		"temporary failure in name resolution",
		"server misbehaving",
	} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

func (b *backend) Close() {
	b.syncMu.Lock()
	b.closed = true
	done, stop := b.syncDone, b.syncCancel
	b.syncMu.Unlock()
	b.once.Do(func() {
		b.cancel()
		if stop != nil {
			stop()
		}
		// Closing the database under a running pass is how a shutdown turns
		// into "database is closed" sync errors, so let the last pass exit
		// first (bounded: it may be blocked in a slow request).
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				slog.Warn("sync did not stop before shutdown")
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = b.httpSrv.Shutdown(ctx)
		if b.db != nil {
			_ = b.db.Close()
		}
		if b.logRestore != nil {
			b.logRestore()
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
