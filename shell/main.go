// Command joplin-lite-desktop is the MyGO desktop shell: a native window
// showing the web UI, backed by the clipper/Data API running in this same
// process. It is the M4 shell described in INTENT.md; the service/ package
// remains the seam a future typed-bindings pass will use.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/egoist/mygo"
)

//go:embed all:webdist
var frontendFS embed.FS

var (
	cfgPath  = flag.String("config", "", "config file (default: ~/.config/go-joplin/config.yaml, else Joplin's settings.json)")
	dataDir  = flag.String("data-dir", "", "database directory to open instead of the configured one")
	noSync   = flag.Bool("no-sync", false, "serve the local database without syncing")
	addrFlag = flag.String("addr", "", "override the API listen address, e.g. 127.0.0.1:41888")

	// nativeUI opens the MyGO ui workspace (no webview) described in
	// docs/native-ui-migration.md.
	nativeUI = flag.Bool("native", false, "open the native workspace instead of the webview")

	// Development-only verification: load the app, probe the live page, save
	// a screenshot and quit. Used by the desktop smoke check.
	smoke   = flag.Bool("smoke", false, "probe the loaded page, capture tmp/desktop-smoke.png, then quit")
	smokeIn = flag.Duration("smoke-after", 6*time.Second, "how long to let the page load before probing")

	// dockIconStatus records the outcome of the Dock.SetIcon call, reported by
	// -smoke so the icon path is verifiable on a real session (SH-11).
	dockIconStatus = "unavailable"
)

func main() {
	flag.Parse()

	mygo.App.SetName("Joplin Lite")
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	var (
		be  *backend
		win *mygo.Window
	)
	mygo.App.OnSecondInstance(func([]string, string) {
		if win != nil {
			win.Show()
			win.Focus()
		}
	})

	mygo.App.WhenReady(func() {
		var err error
		be, err = startBackend(*cfgPath, *dataDir, *noSync, *addrFlag)
		if err != nil {
			showFatal(err)
			return
		}

		if *nativeUI {
			win = openNativeWindow(be)
			win.OnClosed(func() { be.Close() })
			return
		}

		sub, err := fs.Sub(frontendFS, "webdist")
		if err != nil {
			showFatal(err)
			be.Close()
			return
		}
		icon, err := fs.ReadFile(sub, "android-chrome-512x512.png")
		if err == nil {
			err = mygo.App.Dock.SetIcon(icon)
		}
		if err != nil {
			dockIconStatus = err.Error()
			slog.Warn("cannot set application icon", "error", err)
		} else {
			dockIconStatus = "ok"
		}
		handler, err := newFrontendHandler(sub, runtimeConfig{
			Base:    be.baseURL,
			Token:   be.token,
			Shell:   "desktop",
			Profile: be.profile,
		})
		if err != nil {
			showFatal(err)
			be.Close()
			return
		}
		if err := mygo.Protocol.Handle("app", handler); err != nil {
			showFatal(err)
			be.Close()
			return
		}

		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Joplin Lite",
			URL:       "app://localhost/",
			Width:     1400,
			Height:    900,
			MinWidth:  1024,
			MinHeight: 640,
			StateKey:  "main",
		})
		win.OnClosed(func() {
			be.Close()
		})
		if *smoke {
			go runSmoke(win, *smokeIn)
		}
	})

	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if be != nil {
			be.Close()
		}
		os.Exit(1)
	}
}

// runSmoke exercises the real webview: it asks the page for what it rendered,
// checks that the page can reach the in-process API, and saves a screenshot.
func runSmoke(win *mygo.Window, after time.Duration) {
	time.Sleep(after)
	probe, err := win.Page().Eval(`
		const rt = window.__GOJOPLIN__;
		const q = '?token=' + encodeURIComponent(rt.token);
		let api = null, write = null;
		try {
			const res = await fetch(rt.base + '/notes?limit=1&summary=1&token=' + encodeURIComponent(rt.token));
			api = { status: res.status, body: await res.json() };
		} catch (e) {
			api = { error: String(e) };
		}
		try {
			// Exercises the cross-origin preflight the editor and new-note
			// paths depend on, then cleans up after itself.
			const created = await fetch(rt.base + '/notes' + q, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ title: 'desktop smoke', body: 'probe' }),
			});
			const note = created.ok ? await created.json() : {};
			const removed = note.id
				? await fetch(rt.base + '/notes/' + note.id + q, { method: 'DELETE' })
				: null;
			write = { create: created.status, delete: removed ? removed.status : null };
		} catch (e) {
			write = { error: String(e) };
		}
		return {
			title: document.title,
			href: location.href,
			runtime: rt ? rt.shell : null,
			text: document.body.innerText.slice(0, 600),
			api,
			write,
		}`)
	if err != nil {
		slog.Error("desktop smoke: eval failed", "error", err)
	} else {
		if m, ok := probe.(map[string]any); ok {
			m["icon"] = dockIconStatus
		}
		out, _ := json.MarshalIndent(probe, "", "  ")
		fmt.Println(string(out))
		_ = os.WriteFile("tmp/desktop-smoke.json", out, 0o644)
	}
	if png, err := win.CapturePage(); err != nil {
		slog.Error("desktop smoke: capture failed", "error", err)
	} else if err := os.WriteFile("tmp/desktop-smoke.png", png, 0o644); err != nil {
		slog.Error("desktop smoke: write screenshot failed", "error", err)
	}
	mygo.App.Quit()
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
