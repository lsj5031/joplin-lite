package clipper

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall"
)

// Serve runs the clipper HTTP server. A bind failure maps to the explicit
// message: the clipper port (41184) cannot be shared with Joplin desktop —
// the extension ping-scans 41184–41194 and both servers answering would
// split clips arbitrarily.
func Serve(httpSrv *http.Server) error {
	err := httpSrv.ListenAndServe()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	if isAddrInUse(err) {
		return fmt.Errorf("clipper port %s is already in use — Joplin desktop is probably running there; joplin-lite cannot share the clipper port with it: %w",
			httpSrv.Addr, err)
	}
	return err
}

// isAddrInUse detects a bind collision across platforms (Windows reports
// "Only one usage of each socket address ... is normally permitted").
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "address already in use") || strings.Contains(msg, "Only one usage of each socket address")
}
