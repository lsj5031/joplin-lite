// Command joplin-lite-desktop is the native MyGO desktop app: one Go process
// drawing the workspace with mygo/ui over service/, with the clipper/Data API
// in the same process. It embeds no browser and no frontend bundle — the web
// build remains an optional preview through `gojoplin serve --web-dir`.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/egoist/mygo"
)

var (
	cfgPath  = flag.String("config", "", "config file (otherwise remembered launch settings, then normal config defaults)")
	dataDir  = flag.String("data-dir", "", "database directory to open instead of the configured one")
	noSync   = flag.Bool("no-sync", false, "serve the local database without syncing")
	addrFlag = flag.String("addr", "", "override the API listen address, e.g. 127.0.0.1:41185 (the Web Clipper only scans 41184-41193)")

	// Development-only verification: capture the rendered window and quit.
	smoke   = flag.Bool("smoke", false, "capture tmp/native-smoke.png after the first frames, then quit")
	smokeIn = flag.Duration("smoke-after", 6*time.Second, "how long to let the window render before the capture")
)

func main() {
	flag.Parse()

	// DESIGN.md's families, before the first window asks for them.
	registerFonts()

	mygo.App.SetName("Joplin Lite")
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	var (
		be  *backend
		win *mygo.Window
		// workspace is the native UI once the window exists; a link that
		// arrives earlier (the URL the app was launched with) waits in pending.
		workspace *nativeApp
		pending   []string
	)

	// Joplin's external links are joplin://x-callback-url/openNote?id=…, which
	// other apps paste next to the note they came from. The bundle declares the
	// scheme (shell/bundle.sh) and this makes Joplin Lite its handler. Windows
	// packages stay registry-free by design, so there the scheme is not claimed.
	if runtime.GOOS == "darwin" {
		if err := mygo.App.RegisterURLScheme("joplin"); err != nil {
			slog.Warn("cannot register joplin:// links", "error", err)
		}
	}

	openNote := func(id string) {
		if workspace == nil {
			pending = append(pending, id)
			return
		}
		if workspace.openExternalNote(id) {
			slog.Info("opened note from link", "note", id)
			if win != nil {
				win.Show()
				win.Focus()
			}
		}
	}
	mygo.App.OnOpenURL(func(raw string) {
		if id := joplinNoteID(raw); id != "" {
			openNote(id)
			return
		}
		slog.Warn("ignoring unsupported link", "url", raw)
	})

	mygo.App.OnSecondInstance(func([]string, string) {
		if win != nil {
			win.Show()
			win.Focus()
		}
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if win != nil {
			win.Show()
			win.Focus()
		}
	})

	mygo.App.WhenReady(func() {
		var err error
		path, err := launchSettingsPath()
		if err != nil {
			showFatal(err)
			return
		}
		explicit := false
		flag.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "config", "data-dir", "addr", "no-sync":
				explicit = true
			}
		})
		launch, err := resolveLaunch(path, *cfgPath, *dataDir, *addrFlag, explicit)
		if err != nil {
			showFatal(err)
			return
		}
		be, err = startBackend(launch.Config, launch.Profile, *noSync, launch.Addr, launch)
		if err != nil {
			showFatal(err)
			return
		}
		win, workspace = openNativeWindow(be)
		win.OnClosed(func() {
			// The log file outlives the window, so a clean exit is recorded
			// before the backend closes it.
			slog.Info("window closed; shutting down")
			be.Close()
		})
		// Links that arrived while the workspace was being built.
		for _, id := range pending {
			openNote(id)
		}
		pending = nil
	})

	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if be != nil {
			be.Close()
		}
		os.Exit(1)
	}
}

// showFatal reports startup failures as a native dialog, because a packaged
// Windows build has no console to print to, then quits.
func showFatal(err error) {
	slog.Error("cannot start", "error", err)
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Type:  mygo.MessageError,
		Title: "Joplin Lite cannot start", Message: err.Error(),
		Detail: "Check the config file and the configured port, then start the app again.",
	})
	mygo.App.Quit()
}
