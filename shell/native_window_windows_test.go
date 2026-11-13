package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// MyGO's event loop must run on the process main thread. Isolating it also
// prevents its singleton lifecycle from interfering with the headless suite.
func TestMain(m *testing.M) {
	if os.Getenv("JOPLIN_TEST_HIDDEN_HWND") == "1" {
		var checkErr error
		mygo.App.WhenReady(func() {
			opts := mygo.WindowOptions{Title: "Joplin Lite chrome regression", Width: 1280, Height: 800,
				MinWidth: 900, MinHeight: 600, Hidden: true, Content: ui.View(func(c *ui.Context) {})}
			configureWindowsChrome(&opts)
			win := mygo.NewWindow(opts)
			checkErr = checkHiddenWindowsChrome(win.NativeHandle())
			win.Destroy()
			mygo.App.Quit()
		})
		if err := mygo.App.Run(); err != nil {
			checkErr = err
		}
		if checkErr != nil {
			fmt.Fprintln(os.Stderr, checkErr)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestWindowsHiddenNativeChrome(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), "JOPLIN_TEST_HIDDEN_HWND=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real MyGO HWND verification: %v\n%s", err, out)
	}
	t.Log(string(out))
}

var chromeUser32 = syscall.NewLazyDLL("user32.dll")

type chromePoint struct{ X, Y int32 }
type chromeRect struct{ Left, Top, Right, Bottom int32 }

func chromeCall(name string, args ...uintptr) uintptr {
	r, _, _ := chromeUser32.NewProc(name).Call(args...)
	return r
}

func chromeWindowRect(hwnd uintptr) chromeRect {
	var r chromeRect
	chromeCall("GetWindowRect", hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func chromeHit(hwnd uintptr, x, y int32) uintptr {
	return chromeCall("SendMessageW", hwnd, 0x84, 0, uintptr(uint16(x))|uintptr(uint16(y))<<16)
}

func checkHiddenWindowsChrome(hwnd uintptr) error {
	if hwnd == 0 || chromeCall("IsWindow", hwnd) == 0 || chromeCall("IsWindowVisible", hwnd) != 0 {
		return fmt.Errorf("expected a real, invisible HWND, got %#x", hwnd)
	}
	style := chromeCall("GetWindowLongPtrW", hwnd, uintptr(uint32(0xfffffff0))) // GWL_STYLE
	const required = 0x40000 | 0x20000 | 0x10000 | 0x80000                      // resize, minimize, maximize, system menu
	if style&required != required {
		return fmt.Errorf("native resize/window controls lost: style=%#x", style)
	}
	r := chromeWindowRect(hwnd)
	var client chromeRect
	var origin chromePoint
	chromeCall("GetClientRect", hwnd, uintptr(unsafe.Pointer(&client)))
	chromeCall("ClientToScreen", hwnd, uintptr(unsafe.Pointer(&origin)))
	top := origin.Y - r.Top
	if client.Right <= 0 || client.Bottom <= 0 || top < 0 || top > 1 || origin.X <= r.Left || origin.Y+client.Bottom >= r.Bottom {
		return fmt.Errorf("caption must be removed while side/bottom resize borders remain: window=%+v client=%+v origin=%+v", r, client, origin)
	}
	// The backend retains the system side/bottom resize frame, and overlays a
	// separate child HWND for top resizing because native UI fills the client.
	for _, hit := range []struct {
		x, y int32
		want uintptr
	}{
		{r.Left + 1, (r.Top + r.Bottom) / 2, 10},
		{r.Right - 1, (r.Top + r.Bottom) / 2, 11},
		{(r.Left + r.Right) / 2, r.Bottom - 1, 15},
		{r.Left + 1, r.Bottom - 1, 16},
		{r.Right - 1, r.Bottom - 1, 17},
	} {
		if got := chromeHit(hwnd, hit.x, hit.y); got != hit.want {
			return fmt.Errorf("resize hit at %d,%d: got %d, want %d", hit.x, hit.y, got, hit.want)
		}
	}
	var buttons, edge, surface uintptr
	for child := chromeCall("GetWindow", hwnd, 5); child != 0; child = chromeCall("GetWindow", child, 2) {
		var name [128]uint16
		chromeCall("GetClassNameW", child, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
		switch syscall.UTF16ToString(name[:]) {
		case "MyGoCaption":
			cr := chromeWindowRect(child)
			if cr.Right-cr.Left < (r.Right-r.Left)/2 {
				buttons = child
			} else {
				edge = child
			}
		case "MyGoSurface":
			surface = child
		}
	}
	if buttons == 0 || edge == 0 || surface == 0 {
		return fmt.Errorf("missing real backend caption/resize/surface HWNDs: %#x %#x %#x", buttons, edge, surface)
	}
	sr := chromeWindowRect(surface)
	if sr != (chromeRect{origin.X, origin.Y, origin.X + client.Right, origin.Y + client.Bottom}) {
		return fmt.Errorf("native content does not fill caption-free client: %+v", sr)
	}
	br := chromeWindowRect(buttons)
	dpi := chromeCall("GetDpiForWindow", hwnd)
	if br.Right != origin.X+client.Right || br.Top != origin.Y || br.Right-br.Left != int32(138*dpi/96) || br.Bottom-br.Top != int32(48*dpi/96) {
		return fmt.Errorf("caption insets differ from app/capture fixture: %+v at %d DPI", br, dpi)
	}
	for i, want := range []uintptr{8, 9, 20} { // minimize, maximize (snap layouts), close
		x := br.Left + int32(2*i+1)*(br.Right-br.Left)/6
		if got := chromeHit(buttons, x, (br.Top+br.Bottom)/2); got != want {
			return fmt.Errorf("caption control %d hit=%d, want=%d", i, got, want)
		}
	}
	er := chromeWindowRect(edge)
	if chromeHit(edge, er.Left+1, er.Top+1) != 13 || chromeHit(edge, (er.Left+er.Right)/2, er.Top+1) != 12 {
		return fmt.Errorf("top-left/top resize hit tests failed: %+v", er)
	}
	// Exercise WM_NCCALCSIZE's maximized branch without ever showing a window.
	// IsZoomed reads WS_MAXIMIZE. Feed a real monitor rectangle to the actual
	// backend callback and require its client to match that monitor's work area.
	mon := chromeCall("MonitorFromWindow", hwnd, 2)
	mi := struct {
		Size          uint32
		Monitor, Work chromeRect
		Flags         uint32
	}{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	if chromeCall("GetMonitorInfoW", mon, uintptr(unsafe.Pointer(&mi))) == 0 {
		return fmt.Errorf("cannot read monitor work area")
	}
	chromeCall("SetWindowLongPtrW", hwnd, uintptr(uint32(0xfffffff0)), style|0x1000000)
	params := struct {
		Rects [3]chromeRect
		Pos   uintptr
	}{Rects: [3]chromeRect{mi.Monitor}}
	chromeCall("SendMessageW", hwnd, 0x83, 1, uintptr(unsafe.Pointer(&params)))
	chromeCall("SetWindowLongPtrW", hwnd, uintptr(uint32(0xfffffff0)), style)
	if params.Rects[0] != mi.Work {
		return fmt.Errorf("maximized client=%+v, monitor work area=%+v", params.Rects[0], mi.Work)
	}
	if chromeCall("IsWindowVisible", hwnd) != 0 {
		return fmt.Errorf("native verification unexpectedly showed the window")
	}
	fmt.Printf("Invisible HWND: caption top=%dpx, caption insets=138x48 DIP, resize edges/corners and minimize/maximize/close hits passed; maximized NCCALCSIZE=%+v work area\n", top, mi.Work)
	return nil
}
