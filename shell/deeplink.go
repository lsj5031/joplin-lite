package main

import (
	"encoding/hex"
	"net/url"
	"strings"
)

// joplinNoteID returns the note ID of a Joplin external link. Joplin and the
// apps that link to it write these as
// joplin://x-callback-url/openNote?id=<32 hex digits>; the x-callback-url host
// can be omitted, and other x-callback-url parameters (x-success, x-error) are
// allowed. Anything else — another action such as openFolder, another scheme,
// or a malformed ID — returns "".
func joplinNoteID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "joplin") {
		return ""
	}
	action := strings.Trim(u.Path, "/")
	if action == "" {
		// joplin://openNote?id=… puts the action in the host.
		action = u.Host
	}
	if !strings.EqualFold(action, "openNote") {
		return ""
	}
	id := u.Query().Get("id")
	if len(id) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ""
	}
	// Vault IDs are lower-case hex; a link written by hand may not be.
	return strings.ToLower(id)
}
